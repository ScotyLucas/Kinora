package meta

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ScotyLucas/kinora/internal/safe"
	"github.com/ScotyLucas/kinora/internal/store"
)

// The Napló's TMDB side: credits for the director, cast and country breakdowns, and the people and
// collections a challenge can be built from.

// creditsBatch caps one backfill run; the next GET of the Napló picks up the rest.
const creditsBatch = 200

var creditsRunning atomic.Bool

// KickCredits fetches, in the background, the credits of watched titles that have none cached yet.
// Only one run at a time; it is a no-op without a TMDB key.
func (f *Fetcher) KickCredits() {
	if !f.Enabled() || !creditsRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer creditsRunning.Store(false)
		defer safe.Recover(f.log, "journal-credits")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := f.CreditsBackfill(ctx, creditsBatch); err != nil {
			f.log.Warn("journal credits", "err", err)
		}
	}()
}

// CreditsBackfill fetches up to limit missing credits.
func (f *Fetcher) CreditsBackfill(ctx context.Context, limit int) error {
	keys, err := f.st.MissingCredits(limit)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c, err := f.Credits(ctx, k)
		if err != nil && !errors.Is(err, errNotFound) {
			if errors.Is(err, ErrNoKey) || isAuthErr(err) {
				return err
			}
			f.log.Warn("journal credits", "kind", k.Kind, "tmdb", k.TmdbID, "err", err)
			continue
		}
		if err := f.st.PutCredits(k, c); err != nil {
			return err
		}
	}
	return nil
}

func isAuthErr(err error) bool { return err != nil && strings.Contains(err.Error(), "401") }

// Credits fetches the directors (a series' creators), the top of the cast and the production countries.
func (f *Fetcher) Credits(ctx context.Context, k store.CreditKey) (store.Credits, error) {
	type person struct {
		Name  string `json:"name"`
		Job   string `json:"job"`
		Order int    `json:"order"`
	}
	var d struct {
		ProductionCountries []struct {
			ISO string `json:"iso_3166_1"`
		} `json:"production_countries"`
		OriginCountry []string `json:"origin_country"`
		CreatedBy     []person `json:"created_by"`
		Credits       struct {
			Cast []person `json:"cast"`
			Crew []person `json:"crew"`
		} `json:"credits"`
		AggregateCredits struct {
			Cast []person `json:"cast"`
		} `json:"aggregate_credits"`
	}
	path, extra := "/movie/", "credits"
	if k.Kind == "series" {
		path, extra = "/tv/", "aggregate_credits"
	}
	if err := f.get(ctx, path+strconv.FormatInt(k.TmdbID, 10), url.Values{"append_to_response": {extra}}, &d); err != nil {
		return store.Credits{}, err
	}
	c := store.Credits{Directors: []string{}, Cast: []string{}, Countries: []string{}}
	for _, x := range d.ProductionCountries {
		c.Countries = append(c.Countries, x.ISO)
	}
	if len(c.Countries) == 0 {
		c.Countries = append(c.Countries, d.OriginCountry...)
	}
	if k.Kind == "series" {
		for _, p := range d.CreatedBy {
			c.Directors = append(c.Directors, p.Name)
		}
	} else {
		for _, p := range d.Credits.Crew {
			if p.Job == "Director" {
				c.Directors = append(c.Directors, p.Name)
			}
		}
	}
	if len(c.Directors) > 2 {
		c.Directors = c.Directors[:2]
	}
	cast := d.Credits.Cast
	if k.Kind == "series" {
		cast = d.AggregateCredits.Cast
	}
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })
	for i := 0; i < len(cast) && i < 5; i++ {
		c.Cast = append(c.Cast, cast[i].Name)
	}
	return c, nil
}

// Person is a TMDB person a challenge can be built from.
type Person struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Department string `json:"department"` // Directing, Acting, …
	Photo      string `json:"photo,omitempty"`
	KnownFor   string `json:"known_for,omitempty"`
}

// SearchPeople finds people by name.
func (f *Fetcher) SearchPeople(ctx context.Context, query string) ([]Person, error) {
	var r struct {
		Results []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Department  string `json:"known_for_department"`
			ProfilePath string `json:"profile_path"`
			KnownFor    []struct {
				Title string `json:"title"`
				Name  string `json:"name"`
			} `json:"known_for"`
		} `json:"results"`
	}
	if err := f.get(ctx, "/search/person", url.Values{"query": {query}, "include_adult": {"false"}}, &r); err != nil {
		return nil, err
	}
	out := []Person{}
	for _, x := range r.Results {
		p := Person{ID: x.ID, Name: x.Name, Department: x.Department}
		if x.ProfilePath != "" {
			p.Photo = f.ImageURL + "w185" + x.ProfilePath
		}
		for i, k := range x.KnownFor {
			if i == 2 {
				break
			}
			t := k.Title
			if t == "" {
				t = k.Name
			}
			if p.KnownFor != "" {
				p.KnownFor += ", "
			}
			p.KnownFor += t
		}
		out = append(out, p)
	}
	return out, nil
}

// PersonFilms returns the released feature films of a person: directed ones for a director
// (role "directing"), acted in otherwise; oldest first.
func (f *Fetcher) PersonFilms(ctx context.Context, id int64, role string) (string, []store.ChallengeItem, error) {
	type credit struct {
		ID          int64   `json:"id"`
		Title       string  `json:"title"`
		ReleaseDate string  `json:"release_date"`
		PosterPath  string  `json:"poster_path"`
		Job         string  `json:"job"`
		Character   string  `json:"character"`
		GenreIDs    []int   `json:"genre_ids"`
		VoteCount   int     `json:"vote_count"`
		Order       int     `json:"order"`
		Popularity  float64 `json:"popularity"`
	}
	var d struct {
		Name    string `json:"name"`
		Credits struct {
			Cast []credit `json:"cast"`
			Crew []credit `json:"crew"`
		} `json:"movie_credits"`
	}
	if err := f.get(ctx, "/person/"+strconv.FormatInt(id, 10), url.Values{"append_to_response": {"movie_credits"}}, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return "", nil, store.ErrNotFound
		}
		return "", nil, err
	}
	today := time.Now().Format("2006-01-02")
	var list []credit
	if role == "directing" {
		for _, c := range d.Credits.Crew {
			if c.Job == "Director" {
				list = append(list, c)
			}
		}
	} else {
		for _, c := range d.Credits.Cast {
			if c.Order < 15 { // a real role, not a cameo in the end credits
				list = append(list, c)
			}
		}
	}
	out, seen := []store.ChallengeItem{}, map[int64]bool{}
	for _, c := range list {
		// released films with an audience, not documentaries (99) or shorts nobody rated
		if seen[c.ID] || c.ReleaseDate == "" || c.ReleaseDate > today || c.VoteCount < 20 || hasInt(c.GenreIDs, 99) {
			continue
		}
		seen[c.ID] = true
		it := store.ChallengeItem{Kind: "movie", TmdbID: c.ID, Title: c.Title, Year: yearOf(c.ReleaseDate)}
		if c.PosterPath != "" {
			it.Poster = f.ImageURL + "w185" + c.PosterPath
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	if len(out) > store.ChallengeItemsMax {
		out = out[:store.ChallengeItemsMax]
	}
	return d.Name, out, nil
}

func hasInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Collection is a TMDB film series (collection) a challenge can be built from.
type Collection struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Poster string `json:"poster,omitempty"`
}

// SearchCollections finds film series by name.
func (f *Fetcher) SearchCollections(ctx context.Context, query string) ([]Collection, error) {
	var r struct {
		Results []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			PosterPath string `json:"poster_path"`
		} `json:"results"`
	}
	if err := f.get(ctx, "/search/collection", url.Values{"query": {query}}, &r); err != nil {
		return nil, err
	}
	out := []Collection{}
	for _, x := range r.Results {
		c := Collection{ID: x.ID, Name: x.Name}
		if x.PosterPath != "" {
			c.Poster = f.ImageURL + "w185" + x.PosterPath
		}
		out = append(out, c)
	}
	return out, nil
}

// CollectionFilms returns a collection's released films, oldest first.
func (f *Fetcher) CollectionFilms(ctx context.Context, id int64) (string, []store.ChallengeItem, error) {
	var d struct {
		Name  string `json:"name"`
		Parts []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			PosterPath  string `json:"poster_path"`
		} `json:"parts"`
	}
	if err := f.get(ctx, "/collection/"+strconv.FormatInt(id, 10), nil, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return "", nil, store.ErrNotFound
		}
		return "", nil, err
	}
	today := time.Now().Format("2006-01-02")
	out := []store.ChallengeItem{}
	for _, p := range d.Parts {
		if p.ReleaseDate == "" || p.ReleaseDate > today {
			continue
		}
		it := store.ChallengeItem{Kind: "movie", TmdbID: p.ID, Title: p.Title, Year: yearOf(p.ReleaseDate)}
		if p.PosterPath != "" {
			it.Poster = f.ImageURL + "w185" + p.PosterPath
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return d.Name, out, nil
}

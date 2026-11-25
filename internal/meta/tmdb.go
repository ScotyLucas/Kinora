// Package meta fetches optional online metadata from TMDB.
//
// It only runs when an admin configured a TMDB key, never overwrites local NFO
// or manually edited items (tmdb_id = -1), remembers "no match" (tmdb_id = 0)
// so it doesn't hammer the API on every scan, backs off on HTTP 429, and lets
// the admin fix a wrong match by applying a specific TMDB id.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ScotyLucas/kinora/internal/safe"
	"github.com/ScotyLucas/kinora/internal/store"
)

// Fetcher talks to TMDB.
type Fetcher struct {
	st     *store.Store
	imgDir string
	log    *slog.Logger
	hc     *http.Client

	BaseURL  string
	ImageURL string
	OmdbURL  string // "" = DefaultOmdbURL (scores.go)
	Delay    time.Duration

	mu     sync.Mutex
	status Status
	langs  map[string]bool        // viewer languages (metadata_langs), loaded on first use
	again  bool                   // a Kick arrived while a refresh ran: run once more
	noImdb map[int64]bool         // TMDB movies without an IMDb id (asked once per process)
	cache  map[string]cached      // streaming services and genres (discover.go)
	scores map[string]scoresEntry // ratings, budget and gross (scores.go)
}

// Status is the progress of a refresh.
type Status struct {
	Running bool `json:"running"`
	Total   int  `json:"total"`
	Done    int  `json:"done"`
	Matched int  `json:"matched"`
	// Relang is the metadata language matched items are being re-fetched in, set while that runs.
	Relang string `json:"relang,omitempty"`
	// Translate is the viewer language titles are being fetched in, set while that runs.
	Translate  string `json:"translate,omitempty"`
	NoMatch    int    `json:"no_match"`
	LastError  string `json:"last_error,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

// New creates a fetcher writing artwork below imgDir.
func New(st *store.Store, imgDir string, log *slog.Logger) *Fetcher {
	return &Fetcher{st: st, imgDir: imgDir, log: log, hc: &http.Client{Timeout: 20 * time.Second},
		BaseURL: "https://api.themoviedb.org/3", ImageURL: "https://image.tmdb.org/t/p/", Delay: 120 * time.Millisecond}
}

// Status returns a snapshot.
func (f *Fetcher) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// Enabled reports whether a key is configured.
func (f *Fetcher) Enabled() bool { return strings.TrimSpace(f.st.Setting("tmdb_key")) != "" }

// ErrRunning is returned when a refresh is already in progress.
var ErrRunning = errors.New("a metaadat-frissítés már fut")

// ErrNoKey is returned when no TMDB key is configured.
var ErrNoKey = errors.New("nincs megadva TMDB API kulcs (Admin → Metaadat)")

type langCtxKey struct{}

// WithLang makes the TMDB calls made with ctx return texts in lang, a TMDB language as ViewLang gives it
// ("" keeps metadata_lang). For live lookups shown to a viewer (Felfedezés); nothing fetched this way is stored.
func WithLang(ctx context.Context, lang string) context.Context {
	if lang == "" {
		return ctx
	}
	return context.WithValue(ctx, langCtxKey{}, lang)
}

// lang is the TMDB language of a call made with ctx.
func (f *Fetcher) lang(ctx context.Context) string {
	if l, _ := ctx.Value(langCtxKey{}).(string); l != "" {
		return l
	}
	return f.st.Setting("metadata_lang")
}

func (f *Fetcher) get(ctx context.Context, path string, q url.Values, out any) error {
	key := strings.TrimSpace(f.st.Setting("tmdb_key"))
	if key == "" {
		return ErrNoKey
	}
	if q == nil {
		q = url.Values{}
	}
	if q.Get("language") == "" { // translations ask for their own language
		q.Set("language", f.lang(ctx))
	}
	bearer := len(key) > 40
	if !bearer {
		q.Set("api_key", key)
	}
	u := f.BaseURL + path + "?" + q.Encode()
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req.Header.Set("Accept", "application/json")
		if bearer {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := f.hc.Do(req)
		if err != nil {
			return fmt.Errorf("TMDB nem érhető el: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := 2 * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
			f.log.Info("tmdb rate limited", "wait", wait)
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		case resp.StatusCode == http.StatusUnauthorized:
			return errors.New("a TMDB kulcs érvénytelen (401)")
		case resp.StatusCode == http.StatusNotFound:
			return errNotFound
		case resp.StatusCode >= 300:
			return fmt.Errorf("TMDB hiba: HTTP %d", resp.StatusCode)
		}
		if f.Delay > 0 {
			time.Sleep(f.Delay)
		}
		return json.Unmarshal(body, out)
	}
	return errors.New("TMDB: túl sok kérés (429), később próbáld újra")
}

var errNotFound = errors.New("not found")

// ------------------------------------------------------------------ API types

type genre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type movieDetails struct {
	ID           int64   `json:"id"`
	ImdbID       string  `json:"imdb_id"`
	Title        string  `json:"title"`
	ReleaseDate  string  `json:"release_date"`
	Overview     string  `json:"overview"`
	VoteAverage  float64 `json:"vote_average"`
	Genres       []genre `json:"genres"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	Collection   *struct {
		Name string `json:"name"`
	} `json:"belongs_to_collection"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

type tvDetails struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	FirstAirDate   string  `json:"first_air_date"`
	Overview       string  `json:"overview"`
	VoteAverage    float64 `json:"vote_average"`
	Genres         []genre `json:"genres"`
	PosterPath     string  `json:"poster_path"`
	BackdropPath   string  `json:"backdrop_path"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

type seasonDetails struct {
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	PosterPath string `json:"poster_path"`
	Episodes   []struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		StillPath     string  `json:"still_path"`
		VoteAverage   float64 `json:"vote_average"`
	} `json:"episodes"`
}

// SearchResult is a match candidate for the manual "fix match" dialog.
type SearchResult struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	Year     int     `json:"year"`
	Overview string  `json:"overview"`
	Poster   string  `json:"poster,omitempty"`
	Rating   float64 `json:"rating"`
}

type searchResp struct {
	Results []struct {
		ID           int64   `json:"id"`
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Overview     string  `json:"overview"`
		PosterPath   string  `json:"poster_path"`
		VoteAverage  float64 `json:"vote_average"`
	} `json:"results"`
}

func yearOf(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

// Search queries TMDB; kind is "movie" or "series".
func (f *Fetcher) Search(ctx context.Context, kind, query string, year int) ([]SearchResult, error) {
	path := "/search/movie"
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if kind == "series" {
		path = "/search/tv"
		if year > 0 {
			q.Set("first_air_date_year", strconv.Itoa(year))
		}
	} else if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var r searchResp
	if err := f.get(ctx, path, q, &r); err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, x := range r.Results {
		sr := SearchResult{ID: x.ID, Title: x.Title, Year: yearOf(x.ReleaseDate), Overview: x.Overview, Rating: x.VoteAverage}
		if kind == "series" {
			sr.Title, sr.Year = x.Name, yearOf(x.FirstAirDate)
		}
		if x.PosterPath != "" {
			sr.Poster = f.ImageURL + "w185" + x.PosterPath
		}
		out = append(out, sr)
	}
	return out, nil
}

// pickCert returns the first certification by country priority HU > US > DE > GB.
func pickCert(byCountry map[string]string) string {
	for _, c := range []string{"HU", "US", "DE", "GB"} {
		if v := strings.TrimSpace(byCountry[c]); v != "" {
			if c == "US" || c == "GB" {
				return v
			}
			return c + ":" + v
		}
	}
	return ""
}

// ------------------------------------------------------------------ refresh

type target struct {
	id, libID int64
	tmdbID    int64 // known match, re-applied without a search
	kind      string
	title     string
	year      int
}

// appliedLangKey stores the metadata language every matched item was last fetched in.
const appliedLangKey = "metadata_lang_applied"

// artworkRepairKey marks the one-time pass that re-fetches matched items left without artwork:
// a re-scan of a file used to delete the TMDB poster and backdrop of items that have none next
// to the video, and the hearing-impaired subtitle migration re-scanned many files at once.
const artworkRepairKey = "tmdb_artwork_repaired"

// relangNeeded returns the configured metadata language when matched items still carry another one.
// Databases from before this key existed were filled with the default (Hungarian) language.
func (f *Fetcher) relangNeeded() string {
	lang, applied := f.st.Setting("metadata_lang"), f.st.Setting(appliedLangKey)
	if applied == "" {
		applied = store.SettingDefaults["metadata_lang"]
	}
	if lang == applied {
		return ""
	}
	return lang
}

// Refresh fetches metadata for every movie/series without it. retryNoMatch also
// retries items previously marked as "no match". When the metadata language changed
// since the last full pass, already matched items are re-fetched in the new language too.
func (f *Fetcher) Refresh(ctx context.Context, retryNoMatch bool) error {
	if !f.Enabled() {
		return ErrNoKey
	}
	f.mu.Lock()
	if f.status.Running {
		f.mu.Unlock()
		return ErrRunning
	}
	f.status = Status{Running: true}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.status.Running = false
		f.status.FinishedAt = time.Now().Unix()
		f.mu.Unlock()
	}()
	for pass := 0; pass < 4; pass++ {
		if err := f.refresh(ctx, retryNoMatch && pass == 0); err != nil {
			return err
		}
		// the language may have been changed again while this pass ran
		for i := 0; i < 3 && f.relangNeeded() != ""; i++ {
			if err := f.refresh(ctx, false); err != nil {
				return err
			}
		}
		if err := f.translateAll(ctx); err != nil {
			return err
		}
		if err := f.imdbBackfill(ctx); err != nil {
			return err
		}
		f.mu.Lock()
		again := f.again
		f.again = false
		f.mu.Unlock()
		if !again {
			break
		}
	}
	return nil
}

// Kick starts a background refresh, or asks the running one for another pass.
func (f *Fetcher) Kick(ctx context.Context) {
	if !f.Enabled() {
		return
	}
	go func() {
		defer safe.Recover(f.log, "metadata-refresh")
		err := f.Refresh(ctx, false)
		if errors.Is(err, ErrRunning) {
			f.mu.Lock()
			f.again = true
			f.mu.Unlock()
		} else if err != nil {
			f.log.Warn("metadata refresh", "err", err)
		}
	}()
}

func (f *Fetcher) refresh(ctx context.Context, retryNoMatch bool) error {
	relang := f.relangNeeded()
	cond := "tmdb_id IS NULL"
	if retryNoMatch {
		cond = "(tmdb_id IS NULL OR tmdb_id=0)"
	}
	if relang != "" {
		cond = "(" + cond + " OR tmdb_id>0)" // manual / NFO items (-1) are never touched
	}
	repair := relang == "" && f.st.Setting(artworkRepairKey) == ""
	if repair {
		cond = "(" + cond + " OR (tmdb_id>0 AND has_poster=0 AND has_backdrop=0))"
	}
	rows, err := f.st.DB.Query(`SELECT id,library_id,kind,title,COALESCE(year,0),COALESCE(tmdb_id,0) FROM items WHERE kind IN ('movie','series') AND missing=0 AND ` + cond)
	if err != nil {
		return err
	}
	var ts []target
	for rows.Next() {
		var t target
		rows.Scan(&t.id, &t.libID, &t.kind, &t.title, &t.year, &t.tmdbID)
		if t.tmdbID < 0 {
			t.tmdbID = 0
		}
		ts = append(ts, t)
	}
	rows.Close()
	f.mu.Lock()
	f.status = Status{Running: true, Total: len(ts), Relang: relang}
	f.mu.Unlock()
	if relang != "" {
		f.log.Info("tmdb: re-fetching metadata in the new language", "lang", relang, "items", len(ts))
	}
	libs := map[int64]bool{}
	for _, t := range ts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		matched, err := f.fetchOne(ctx, t, t.tmdbID)
		f.mu.Lock()
		f.status.Done++
		if err != nil {
			f.status.LastError = t.title + ": " + err.Error()
		} else if matched {
			f.status.Matched++
		} else {
			f.status.NoMatch++
		}
		f.mu.Unlock()
		if err != nil {
			f.log.Warn("tmdb", "title", t.title, "err", err)
			if errors.Is(err, ErrNoKey) || strings.Contains(err.Error(), "401") {
				return err
			}
			continue
		}
		libs[t.libID] = true
	}
	for id := range libs {
		f.st.FinishScan(id) // propagate series ratings to episodes
	}
	if relang != "" {
		if err := f.st.SetSetting(appliedLangKey, relang); err != nil {
			return err
		}
		f.st.ClearGenreLabels() // they map the old language's genre names
	}
	if repair {
		if err := f.st.SetSetting(artworkRepairKey, "1"); err != nil {
			return err
		}
	}
	f.log.Info("tmdb refresh finished", "items", len(ts))
	return nil
}

// Match applies a specific TMDB id to an item (manual "fix match").
func (f *Fetcher) Match(ctx context.Context, itemID, tmdbID int64) error {
	var t target
	err := f.st.DB.QueryRow(`SELECT id,library_id,kind,title,COALESCE(year,0) FROM items WHERE id=? AND kind IN ('movie','series')`, itemID).
		Scan(&t.id, &t.libID, &t.kind, &t.title, &t.year)
	if err != nil {
		return store.ErrNotFound
	}
	if _, err := f.fetchOne(ctx, t, tmdbID); err != nil {
		return err
	}
	// the old translations belong to the previous match
	if err := f.st.ClearTranslations(t.id); err != nil {
		return err
	}
	for _, lang := range f.viewerLangs() {
		if err := f.translateOne(ctx, t.id, t.kind, tmdbID, lang); err != nil {
			f.log.Warn("tmdb translation", "title", t.title, "lang", lang, "err", err)
		}
	}
	return f.st.FinishScan(t.libID)
}

func (f *Fetcher) fetchOne(ctx context.Context, t target, forceID int64) (bool, error) {
	id := forceID
	if id == 0 {
		res, err := f.Search(ctx, t.kind, t.title, t.year)
		if err != nil {
			return false, err
		}
		if len(res) == 0 && t.year > 0 {
			if res, err = f.Search(ctx, t.kind, t.title, 0); err != nil {
				return false, err
			}
		}
		if len(res) == 0 {
			return false, f.st.SetTmdbID(t.id, 0) // remember: don't retry on every scan
		}
		best := res[0]
		for _, r := range res { // prefer an exact year match
			if t.year > 0 && r.Year == t.year {
				best = r
				break
			}
		}
		id = best.ID
	}
	if t.kind == "series" {
		return true, f.applySeries(ctx, t, id)
	}
	return true, f.applyMovie(ctx, t, id)
}

func (f *Fetcher) applyMovie(ctx context.Context, t target, id int64) error {
	var d movieDetails
	if err := f.get(ctx, fmt.Sprintf("/movie/%d", id), url.Values{"append_to_response": {"release_dates"}}, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return fmt.Errorf("a TMDB azonosító (%d) nem létezik", id)
		}
		return err
	}
	certs := map[string]string{}
	for _, r := range d.ReleaseDates.Results {
		for _, x := range r.Dates {
			if x.Certification != "" && certs[r.Country] == "" {
				certs[r.Country] = x.Certification
			}
		}
	}
	m := store.OnlineMeta{TmdbID: d.ID, Title: d.Title, Year: yearOf(d.ReleaseDate), Overview: d.Overview,
		ContentRating: pickCert(certs)}
	if d.VoteAverage > 0 {
		m.Rating = &d.VoteAverage
	}
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	if d.Collection != nil && d.Collection.Name != "" {
		m.Tags = append(m.Tags, d.Collection.Name)
	}
	if err := f.st.ApplyOnline(t.id, m); err != nil {
		return err
	}
	if err := f.st.SetImdbID(t.id, d.ImdbID); err != nil {
		return err
	}
	f.artwork(ctx, t.id, d.PosterPath, d.BackdropPath)
	return nil
}

func (f *Fetcher) applySeries(ctx context.Context, t target, id int64) error {
	var d tvDetails
	if err := f.get(ctx, fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {"content_ratings"}}, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return fmt.Errorf("a TMDB azonosító (%d) nem létezik", id)
		}
		return err
	}
	certs := map[string]string{}
	for _, r := range d.ContentRatings.Results {
		certs[r.Country] = r.Rating
	}
	m := store.OnlineMeta{TmdbID: d.ID, Title: d.Name, Year: yearOf(d.FirstAirDate), Overview: d.Overview, ContentRating: pickCert(certs)}
	if d.VoteAverage > 0 {
		m.Rating = &d.VoteAverage
	}
	for _, g := range d.Genres {
		// TMDB tv genres look like "Sci-Fi & Fantasy"
		for _, part := range strings.Split(g.Name, " & ") {
			m.Genres = append(m.Genres, strings.TrimSpace(part))
		}
	}
	if err := f.st.ApplyOnline(t.id, m); err != nil {
		return err
	}
	f.artwork(ctx, t.id, d.PosterPath, d.BackdropPath)

	seasons, err := f.st.SeasonsOf(t.id)
	if err != nil {
		return err
	}
	for _, sn := range seasons {
		var sd seasonDetails
		if err := f.get(ctx, fmt.Sprintf("/tv/%d/season/%d", d.ID, sn.Num), nil, &sd); err != nil {
			if errors.Is(err, errNotFound) {
				continue
			}
			return err
		}
		if sd.PosterPath != "" {
			if f.image(ctx, "w500", sd.PosterPath, filepath.Join(f.imgDir, fmt.Sprint(sn.ID), "poster.jpg")) {
				_, b := f.st.ImageFlags(sn.ID)
				f.st.SetImageFlags(sn.ID, true, b)
			}
		}
		for _, ep := range sd.Episodes {
			eid := f.st.EpisodeID(t.id, sn.Num, ep.EpisodeNumber)
			if eid == 0 {
				continue
			}
			f.st.ApplyEpisode(eid, d.ID, ep.Name, ep.Overview)
			if ep.StillPath != "" {
				f.image(ctx, "w300", ep.StillPath, filepath.Join(f.imgDir, fmt.Sprint(eid), "thumb.jpg"))
			}
		}
	}
	return nil
}

func (f *Fetcher) artwork(ctx context.Context, id int64, poster, backdrop string) {
	hasP, hasB := f.st.ImageFlags(id)
	dir := filepath.Join(f.imgDir, fmt.Sprint(id))
	if !hasP && f.image(ctx, "w500", poster, filepath.Join(dir, "poster.jpg")) {
		hasP = true
	}
	if !hasB && f.image(ctx, "w1280", backdrop, filepath.Join(dir, "backdrop.jpg")) {
		hasB = true
	}
	f.st.SetImageFlags(id, hasP, hasB)
}

func (f *Fetcher) image(ctx context.Context, size, path, dst string) bool {
	if path == "" {
		return false
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", f.ImageURL+size+path, nil)
	resp, err := f.hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return false
	}
	_, err = io.Copy(out, io.LimitReader(resp.Body, 10<<20))
	out.Close()
	if err != nil {
		os.Remove(tmp)
		return false
	}
	return os.Rename(tmp, dst) == nil
}

// imdbBackfill fetches the IMDb id of matched movies that have none yet (matched before
// Kinora stored it, or matched through an NFO tmdbid). The versions of a film are grouped by it.
func (f *Fetcher) imdbBackfill(ctx context.Context) error {
	rows, err := f.st.DB.Query(`SELECT id,tmdb_id FROM items WHERE kind='movie' AND missing=0 AND tmdb_id>0 AND imdb_id=''`)
	if err != nil {
		return err
	}
	type row struct{ id, tmdb int64 }
	var todo []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.tmdb)
		todo = append(todo, r)
	}
	rows.Close()
	known := map[int64]string{}
	for _, r := range todo {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		f.mu.Lock()
		skip := f.noImdb[r.tmdb]
		f.mu.Unlock()
		if skip {
			continue
		}
		imdb, ok := known[r.tmdb]
		if !ok {
			var d struct {
				ImdbID string `json:"imdb_id"`
			}
			if err := f.get(ctx, fmt.Sprintf("/movie/%d", r.tmdb), url.Values{}, &d); err != nil {
				if errors.Is(err, errNotFound) {
					continue
				}
				if errors.Is(err, ErrNoKey) || strings.Contains(err.Error(), "401") {
					return err
				}
				f.log.Warn("tmdb imdb id", "tmdb", r.tmdb, "err", err)
				continue
			}
			imdb = store.NormalizeImdbID(d.ImdbID)
			known[r.tmdb] = imdb
		}
		if imdb == "" {
			f.mu.Lock()
			if f.noImdb == nil {
				f.noImdb = map[int64]bool{}
			}
			f.noImdb[r.tmdb] = true
			f.mu.Unlock()
			continue
		}
		if err := f.st.SetImdbID(r.id, imdb); err != nil {
			return err
		}
	}
	return nil
}

// MovieInfo is what the acquisition module needs about a film: the ids to search by and
// every title a release name may use.
type MovieInfo struct {
	ID            int64    `json:"id"`
	IMDb          string   `json:"imdb_id"`
	Title         string   `json:"title"`          // in the server's metadata language
	OriginalTitle string   `json:"original_title"` // release names usually carry this…
	EnglishTitle  string   `json:"english_title"`  // …or the English one
	Year          int      `json:"year"`
	Overview      string   `json:"overview"`
	Poster        string   `json:"poster,omitempty"`
	Backdrop      string   `json:"backdrop,omitempty"`
	Rating        float64  `json:"rating"`
	Runtime       int      `json:"runtime"`
	Genres        []string `json:"genres"`
}

// Movie fetches one film by its TMDB id (in the metadata language, plus the English title).
func (f *Fetcher) Movie(ctx context.Context, id int64) (MovieInfo, error) {
	var d struct {
		movieDetails
		OriginalTitle string `json:"original_title"`
		Runtime       int    `json:"runtime"`
	}
	if err := f.get(ctx, "/movie/"+strconv.FormatInt(id, 10), nil, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return MovieInfo{}, errors.New("a film nem található a TMDB-ben")
		}
		return MovieInfo{}, err
	}
	m := MovieInfo{ID: d.ID, IMDb: d.ImdbID, Title: d.Title, OriginalTitle: d.OriginalTitle, Year: yearOf(d.ReleaseDate),
		Overview: d.Overview, Rating: d.VoteAverage, Runtime: d.Runtime, Genres: []string{}}
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	if d.PosterPath != "" {
		m.Poster = f.ImageURL + "w342" + d.PosterPath
	}
	if d.BackdropPath != "" {
		m.Backdrop = f.ImageURL + "w1280" + d.BackdropPath
	}
	m.EnglishTitle = m.Title
	if !strings.HasPrefix(f.st.Setting("metadata_lang"), "en") {
		var en struct {
			Title string `json:"title"`
		}
		if err := f.get(ctx, "/movie/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}}, &en); err == nil && en.Title != "" {
			m.EnglishTitle = en.Title
		}
	}
	return m, nil
}

// MovieLists are the TMDB film lists the discover page shows.
var MovieLists = map[string]string{
	"trending": "/trending/movie/week",
	"popular":  "/movie/popular",
	"upcoming": "/movie/upcoming",
	"top":      "/movie/top_rated",
}

// MovieList returns one page of a TMDB film list (see MovieLists).
func (f *Fetcher) MovieList(ctx context.Context, list string, page int) ([]SearchResult, error) {
	path, ok := MovieLists[list]
	if !ok {
		return nil, errors.New("ismeretlen lista")
	}
	q := url.Values{}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if lang := f.st.Setting("metadata_lang"); len(lang) == 5 && list == "upcoming" {
		q.Set("region", lang[3:]) // "hu-HU" → releases in Hungary
	}
	var r searchResp
	if err := f.get(ctx, path, q, &r); err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, x := range r.Results {
		sr := SearchResult{ID: x.ID, Title: x.Title, Year: yearOf(x.ReleaseDate), Overview: x.Overview, Rating: x.VoteAverage}
		if x.PosterPath != "" {
			sr.Poster = f.ImageURL + "w342" + x.PosterPath
		}
		out = append(out, sr)
	}
	return out, nil
}

// ------------------------------------------------------------------ series (acquisition M3)

// TVSeason is one season of a series as TMDB lists it.
type TVSeason struct {
	Number       int    `json:"number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
}

// TVInfo is what the acquisition module needs about a series.
type TVInfo struct {
	ID            int64      `json:"id"`
	IMDb          string     `json:"imdb_id"`
	TVDB          int64      `json:"tvdb_id"`
	Title         string     `json:"title"`
	OriginalTitle string     `json:"original_title"`
	EnglishTitle  string     `json:"english_title"`
	Year          int        `json:"year"`
	Overview      string     `json:"overview"`
	Poster        string     `json:"poster,omitempty"`
	Backdrop      string     `json:"backdrop,omitempty"`
	Ended         bool       `json:"ended"`
	Genres        []string   `json:"genres"`
	Seasons       []TVSeason `json:"seasons"`
}

// TVEpisode is one episode with its first air date (YYYY-MM-DD, empty when unknown).
type TVEpisode struct {
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	AirDate string `json:"air_date"`
}

// TV fetches a series by its TMDB id with its seasons and external ids.
func (f *Fetcher) TV(ctx context.Context, id int64) (TVInfo, error) {
	var d struct {
		tvDetails
		OriginalName string `json:"original_name"`
		Status       string `json:"status"`
		Seasons      []struct {
			SeasonNumber int    `json:"season_number"`
			Name         string `json:"name"`
			EpisodeCount int    `json:"episode_count"`
			AirDate      string `json:"air_date"`
		} `json:"seasons"`
		ExternalIDs struct {
			IMDb string `json:"imdb_id"`
			TVDB int64  `json:"tvdb_id"`
		} `json:"external_ids"`
	}
	if err := f.get(ctx, "/tv/"+strconv.FormatInt(id, 10), url.Values{"append_to_response": {"external_ids"}}, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return TVInfo{}, errors.New("a sorozat nem található a TMDB-ben")
		}
		return TVInfo{}, err
	}
	t := TVInfo{ID: d.ID, IMDb: d.ExternalIDs.IMDb, TVDB: d.ExternalIDs.TVDB, Title: d.Name, OriginalTitle: d.OriginalName,
		Year: yearOf(d.FirstAirDate), Overview: d.Overview, Ended: d.Status == "Ended" || d.Status == "Canceled", Genres: []string{}, Seasons: []TVSeason{}}
	for _, g := range d.Genres {
		t.Genres = append(t.Genres, g.Name)
	}
	for _, s := range d.Seasons {
		t.Seasons = append(t.Seasons, TVSeason{Number: s.SeasonNumber, Name: s.Name, EpisodeCount: s.EpisodeCount, AirDate: s.AirDate})
	}
	if d.PosterPath != "" {
		t.Poster = f.ImageURL + "w342" + d.PosterPath
	}
	if d.BackdropPath != "" {
		t.Backdrop = f.ImageURL + "w1280" + d.BackdropPath
	}
	t.EnglishTitle = t.Title
	if !strings.HasPrefix(f.st.Setting("metadata_lang"), "en") {
		var en struct {
			Name string `json:"name"`
		}
		if err := f.get(ctx, "/tv/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}}, &en); err == nil && en.Name != "" {
			t.EnglishTitle = en.Name
		}
	}
	return t, nil
}

// TVSeasonEpisodes lists the episodes of one season.
func (f *Fetcher) TVSeasonEpisodes(ctx context.Context, id int64, season int) ([]TVEpisode, error) {
	var d struct {
		Episodes []struct {
			EpisodeNumber int    `json:"episode_number"`
			Name          string `json:"name"`
			AirDate       string `json:"air_date"`
		} `json:"episodes"`
	}
	if err := f.get(ctx, "/tv/"+strconv.FormatInt(id, 10)+"/season/"+strconv.Itoa(season), nil, &d); err != nil {
		return nil, err
	}
	out := make([]TVEpisode, 0, len(d.Episodes))
	for _, e := range d.Episodes {
		out = append(out, TVEpisode{Season: season, Episode: e.EpisodeNumber, Title: e.Name, AirDate: e.AirDate})
	}
	return out, nil
}

// TVLists are the TMDB series lists the discover page shows.
var TVLists = map[string]string{
	"tv_trending": "/trending/tv/week",
	"tv_popular":  "/tv/popular",
	"tv_airing":   "/tv/on_the_air",
}

// TVList returns one page of a TMDB series list (see TVLists).
func (f *Fetcher) TVList(ctx context.Context, list string, page int) ([]SearchResult, error) {
	path, ok := TVLists[list]
	if !ok {
		return nil, errors.New("ismeretlen lista")
	}
	q := url.Values{}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	var r searchResp
	if err := f.get(ctx, path, q, &r); err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, x := range r.Results {
		sr := SearchResult{ID: x.ID, Title: x.Name, Year: yearOf(x.FirstAirDate), Overview: x.Overview, Rating: x.VoteAverage}
		if x.PosterPath != "" {
			sr.Poster = f.ImageURL + "w342" + x.PosterPath
		}
		out = append(out, sr)
	}
	return out, nil
}

// FindByExternal finds the TMDB id of a series ("tv") or film ("movie") by an id of another catalogue:
// source is "tvdb_id" or "imdb_id". 0 when TMDB knows none.
func (f *Fetcher) FindByExternal(ctx context.Context, kind, source, id string) (int64, error) {
	if (source != "tvdb_id" && source != "imdb_id") || strings.TrimSpace(id) == "" {
		return 0, nil
	}
	var d struct {
		Movies []struct {
			ID int64 `json:"id"`
		} `json:"movie_results"`
		TV []struct {
			ID int64 `json:"id"`
		} `json:"tv_results"`
	}
	if err := f.get(ctx, "/find/"+url.PathEscape(strings.TrimSpace(id)), url.Values{"external_source": {source}}, &d); err != nil {
		if errors.Is(err, errNotFound) {
			return 0, nil
		}
		return 0, err
	}
	switch {
	case kind == "tv" && len(d.TV) > 0:
		return d.TV[0].ID, nil
	case kind == "movie" && len(d.Movies) > 0:
		return d.Movies[0].ID, nil
	}
	return 0, nil
}

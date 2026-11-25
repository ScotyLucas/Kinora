package meta

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ScotyLucas/kinora/internal/store"
)

func TestRefreshMovieAndSeries(t *testing.T) {
	var hits429 atomic.Int32
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/3/search/movie", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k123" || r.URL.Query().Get("language") != "hu-HU" {
			w.WriteHeader(401)
			return
		}
		if hits429.Add(1) == 1 { // first call is rate limited
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		switch r.URL.Query().Get("query") {
		case "Arrival":
			j(w, map[string]any{"results": []map[string]any{{"id": 1, "title": "Wrong", "release_date": "1996-01-01"}, {"id": 329865, "title": "Érkezés", "release_date": "2016-11-10"}}})
		default:
			j(w, map[string]any{"results": []any{}})
		}
	})
	en := func(r *http.Request) bool { return r.URL.Query().Get("language") == "en-US" }
	mux.HandleFunc("/3/movie/329865", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"id": 329865, "title": "Arrival", "release_date": "2016-11-10", "overview": "A linguist."})
			return
		}
		j(w, map[string]any{"id": 329865, "title": "Érkezés", "release_date": "2016-11-10", "overview": "Nyelvész.", "vote_average": 7.6,
			"genres": []map[string]any{{"name": "Sci-Fi"}, {"name": "Dráma"}}, "belongs_to_collection": map[string]any{"name": "Villeneuve gyűjtemény"},
			"release_dates": map[string]any{"results": []map[string]any{
				{"iso_3166_1": "US", "release_dates": []map[string]any{{"certification": "PG-13"}}},
				{"iso_3166_1": "HU", "release_dates": []map[string]any{{"certification": "12"}}}}}})
	})
	mux.HandleFunc("/3/search/tv", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"results": []map[string]any{{"id": 77, "name": "Teszt Sorozat", "first_air_date": "2020-01-01"}}})
	})
	mux.HandleFunc("/3/tv/77", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"id": 77, "name": "Test Show", "first_air_date": "2020-01-01", "overview": "Description",
				"seasons": []map[string]any{{"season_number": 1}}})
			return
		}
		j(w, map[string]any{"id": 77, "name": "Teszt Sorozat", "first_air_date": "2020-01-01", "overview": "Leírás", "vote_average": 8.2,
			"genres":          []map[string]any{{"name": "Sci-Fi & Fantasy"}},
			"content_ratings": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "rating": "TV-MA"}}}})
	})
	mux.HandleFunc("/3/tv/77/season/1", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"episodes": []map[string]any{{"episode_number": 1, "name": "The beginning"}}})
			return
		}
		j(w, map[string]any{"episodes": []map[string]any{{"episode_number": 1, "name": "A kezdet", "overview": "Első rész."}}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSetting("tmdb_key", "k123")
	lib, _ := st.CreateLibrary("x", "/x", "mixed")
	ins := func(kind, key, title string, year int, tmdb any) int64 {
		res, err := st.DB.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,year,tmdb_id,added_at,updated_at) VALUES(?,?,?,?,?,?,?,0,0)`,
			lib.ID, kind, key, title, title, year, tmdb)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	movie := ins("movie", "m", "Arrival", 2016, nil)
	nfo := ins("movie", "n", "Local NFO", 2000, -1)
	unknown := ins("movie", "u", "Nothing Like This", 2001, nil)
	series := ins("series", "s", "Test Show", 2020, nil)
	st.DB.Exec(`UPDATE items SET series_id=? WHERE id=?`, series, series)
	season := ins("season", "s/s1", "1. évad", 0, nil)
	st.DB.Exec(`UPDATE items SET parent_id=?,series_id=?,season_num=1 WHERE id=?`, series, series, season)
	ep := ins("episode", "e", "1. rész", 0, nil)
	st.DB.Exec(`UPDATE items SET parent_id=?,series_id=?,season_num=1,episode_num=1 WHERE id=?`, season, series, ep)

	f := New(st, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.BaseURL, f.ImageURL, f.Delay = ts.URL+"/3", ts.URL+"/img/", 0
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	it, _ := st.ItemFull(movie, nil)
	if it.Title != "Érkezés" || *it.TmdbID != 329865 || it.ContentRating != "HU:12" || *it.RatingLevel != 2 || len(it.Genres) != 2 {
		t.Fatalf("movie: %+v", it)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "Villeneuve gyűjtemény" {
		t.Fatalf("collection tag: %v", it.Tags)
	}
	if n, _ := st.Item(nfo, nil); n.Title != "Local NFO" {
		t.Fatal("NFO item overwritten")
	}
	if u, _ := st.Item(unknown, nil); u.TmdbID == nil || *u.TmdbID != 0 {
		t.Fatal("no-match must be remembered as 0")
	}
	sr, _ := st.ItemFull(series, nil)
	if sr.Title != "Teszt Sorozat" || *sr.RatingLevel != 3 || len(sr.Genres) != 2 {
		t.Fatalf("series: %+v %v", sr, sr.Genres)
	}
	e, _ := st.Item(ep, nil)
	if e.Title != "A kezdet" || e.RatingLevel == nil || *e.RatingLevel != 3 {
		t.Fatalf("episode: %+v", e)
	}
	if st := f.Status(); st.Matched != 2 || st.NoMatch != 1 || st.Running {
		t.Fatalf("status: %+v", st)
	}
	// manual fix
	if err := f.Match(context.Background(), unknown, 329865); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.Item(unknown, nil); u.Title != "Érkezés" {
		t.Fatal("manual match")
	}

	// switching the metadata language re-fetches matched items, but never NFO / manual ones
	st.SetSetting("metadata_lang", "en-US")
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]string{movie: "Arrival", unknown: "Arrival", series: "Test Show", ep: "The beginning", nfo: "Local NFO"} {
		if it, _ := st.Item(id, nil); it.Title != want {
			t.Fatalf("after language switch item %d: %q, want %q", id, it.Title, want)
		}
	}
	if st.Setting("metadata_lang_applied") != "en-US" {
		t.Fatal("applied language not stored")
	}
	if err := f.Refresh(context.Background(), false); err != nil || f.Status().Total != 0 {
		t.Fatalf("second refresh must not re-fetch again: %+v %v", f.Status(), err)
	}
}

func TestViewerLanguage(t *testing.T) {
	var enHits atomic.Int32
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	en := func(r *http.Request) bool {
		if r.URL.Query().Get("language") == "en-US" {
			enHits.Add(1)
			return true
		}
		return false
	}
	mux.HandleFunc("/3/search/movie", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"results": []map[string]any{{"id": 10, "title": "Érkezés", "release_date": "2016-11-10"}}})
	})
	mux.HandleFunc("/3/movie/10", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"id": 10, "title": "Arrival", "overview": "A linguist."})
			return
		}
		j(w, map[string]any{"id": 10, "title": "Érkezés", "release_date": "2016-11-10", "overview": "Nyelvész.",
			"genres": []map[string]any{{"id": 18, "name": "Dráma"}}})
	})
	mux.HandleFunc("/3/search/tv", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"results": []map[string]any{{"id": 77, "name": "Teszt Sorozat", "first_air_date": "2020-01-01"}}})
	})
	mux.HandleFunc("/3/tv/77", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"id": 77, "name": "Test Show", "overview": "Description"})
			return
		}
		j(w, map[string]any{"id": 77, "name": "Teszt Sorozat", "first_air_date": "2020-01-01", "overview": "Leírás",
			"genres": []map[string]any{{"id": 10765, "name": "Sci-Fi & Fantasy"}}})
	})
	mux.HandleFunc("/3/tv/77/season/1", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"name": "Season 1", "episodes": []map[string]any{{"episode_number": 1, "name": "The beginning", "overview": "First."}}})
			return
		}
		j(w, map[string]any{"name": "1. évad", "episodes": []map[string]any{{"episode_number": 1, "name": "A kezdet", "overview": "Első rész."}}})
	})
	mux.HandleFunc("/3/genre/movie/list", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"genres": []map[string]any{{"id": 18, "name": "Drama"}}})
			return
		}
		j(w, map[string]any{"genres": []map[string]any{{"id": 18, "name": "Dráma"}}})
	})
	mux.HandleFunc("/3/genre/tv/list", func(w http.ResponseWriter, r *http.Request) {
		if en(r) {
			j(w, map[string]any{"genres": []map[string]any{{"id": 10765, "name": "Science Fiction & Fantasy"}}})
			return
		}
		j(w, map[string]any{"genres": []map[string]any{{"id": 10765, "name": "Sci-Fi & Fantasy"}}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib, _ := st.CreateLibrary("x", "/x", "mixed")
	ins := func(kind, key, title string, tmdb any) int64 {
		res, err := st.DB.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,tmdb_id,added_at,updated_at) VALUES(?,?,?,?,?,?,0,0)`,
			lib.ID, kind, key, title, title, tmdb)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	movie := ins("movie", "m", "Arrival", nil)
	nfo := ins("movie", "n", "Helyi NFO", -1)
	series := ins("series", "s", "Test Show", nil)
	season := ins("season", "s/s1", "1. évad", nil)
	st.DB.Exec(`UPDATE items SET parent_id=?,series_id=?,season_num=1 WHERE id=?`, series, series, season)
	ep := ins("episode", "e", "1. rész", nil)
	st.DB.Exec(`UPDATE items SET parent_id=?,series_id=?,season_num=1,episode_num=1 WHERE id=?`, season, series, ep)

	f := New(st, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.BaseURL, f.ImageURL, f.Delay = ts.URL+"/3", ts.URL+"/img/", 0

	// without a TMDB key nothing is kicked off, but the language is mapped and registered
	for code, want := range map[string]string{"en": "en-US", "EN-gb": "en-US", "hu": "", "fr": "fr", "pt-br": "pt-BR", "x y": "", "": ""} {
		if got := f.ViewLang(context.Background(), code); got != want {
			t.Fatalf("ViewLang(%q) = %q, want %q", code, got, want)
		}
	}
	if got := st.Setting("metadata_langs"); got != "en-US,fr,pt-BR" {
		t.Fatalf("registered languages: %q", got)
	}
	st.SetSetting("metadata_langs", "en-US")
	f.langs = nil
	st.SetSetting("tmdb_key", "k")
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	enP := &store.Profile{MetaLang: "en-US"}
	m, _ := st.ItemFull(movie, enP)
	if m.Title != "Arrival" || m.Overview != "A linguist." || len(m.Genres) != 1 || m.Genres[0] != "Drama" {
		t.Fatalf("movie in English: %+v %v", m, m.Genres)
	}
	if m, _ := st.ItemFull(movie, nil); m.Title != "Érkezés" || m.Genres[0] != "Dráma" {
		t.Fatalf("stored language changed: %+v", m)
	}
	if n, _ := st.Item(nfo, enP); n.Title != "Helyi NFO" {
		t.Fatal("NFO item translated")
	}
	sr, _ := st.ItemFull(series, enP)
	if sr.Title != "Test Show" || sr.Overview != "Description" || strings.Join(sr.Genres, ",") != "Fantasy,Science Fiction" {
		t.Fatalf("series in English: %+v %v", sr, sr.Genres)
	}
	if s, _ := st.Item(season, enP); s.Title != "Season 1" {
		t.Fatalf("season in English: %q", s.Title)
	}
	if e, _ := st.Item(ep, enP); e.Title != "The beginning" || e.Overview != "First." || e.SeriesTitle != "Test Show" {
		t.Fatalf("episode in English: %+v", e)
	}
	if res, _ := st.Search(enP, "linguist", 10); len(res) != 1 || res[0].ID != movie {
		t.Fatalf("search in English: %v", res)
	}
	genres, _, _ := st.Facets(enP, 0)
	var drama store.Facet
	for _, g := range genres {
		if g.Value == "Dráma" {
			drama = g
		}
	}
	if drama.Label != "Drama" {
		t.Fatalf("genre facets: %+v", genres)
	}

	// nothing is fetched again until a new title or episode shows up
	before := enHits.Load()
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if enHits.Load() != before {
		t.Fatal("translations fetched again")
	}
	ep2 := ins("episode", "e2", "2. rész", nil)
	st.DB.Exec(`UPDATE items SET parent_id=?,series_id=?,season_num=1,episode_num=2 WHERE id=?`, season, series, ep2)
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after := enHits.Load()
	if after == before {
		t.Fatal("new episode did not trigger a translation pass")
	}
	if err := f.Refresh(context.Background(), false); err != nil || enHits.Load() != after {
		t.Fatalf("episode unknown to TMDB is asked for again (%v)", err)
	}
}

// Matched items left without artwork get it back once; later refreshes leave them alone.
func TestRefreshRepairsLostArtworkOnce(t *testing.T) {
	var details atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/3/movie/5", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("append_to_response") != "" { // not the IMDb id backfill
			details.Add(1)
		}
		w.Write([]byte(`{"id":5,"title":"Örökség","release_date":"2018-06-08","poster_path":"/p.jpg","backdrop_path":"/b.jpg"}`))
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("jpg")) })
	ts := httptest.NewServer(mux)
	defer ts.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSetting("tmdb_key", "k123")
	lib, _ := st.CreateLibrary("x", "/x", "movies")
	res, err := st.DB.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,year,tmdb_id,added_at,updated_at) VALUES(?,?,?,?,?,?,?,0,0)`,
		lib.ID, "movie", "m", "Hereditary", "Hereditary", 2018, 5)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	imgs := t.TempDir()
	f := New(st, imgs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.BaseURL, f.ImageURL, f.Delay = ts.URL+"/3", ts.URL+"/img/", 0
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if p, b := st.ImageFlags(id); !p || !b {
		t.Fatalf("artwork not repaired: poster=%v backdrop=%v", p, b)
	}
	st.SetImageFlags(id, false, false)
	if err := f.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if n := details.Load(); n != 1 {
		t.Fatalf("the repair must run once, details fetched %d times", n)
	}
}

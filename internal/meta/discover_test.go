package meta

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ScotyLucas/kinora/internal/store"
)

func TestBrowseProvidersWhereToWatch(t *testing.T) {
	var provHits atomic.Int32
	var last url.Values
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/3/watch/providers/movie", func(w http.ResponseWriter, r *http.Request) {
		provHits.Add(1)
		if r.URL.Query().Get("watch_region") != "HU" {
			t.Errorf("provider region: %q", r.URL.RawQuery)
		}
		j(w, map[string]any{"results": []map[string]any{
			{"provider_id": 1899, "provider_name": "HBO Max", "logo_path": "/hbo.jpg", "display_priority": 9, "display_priorities": map[string]int{"HU": 2}},
			{"provider_id": 8, "provider_name": "Netflix", "logo_path": "/nf.jpg", "display_priority": 0, "display_priorities": map[string]int{"HU": 1}},
			{"provider_id": 337, "provider_name": "Disney Plus", "display_priority": 5},
		}})
	})
	mux.HandleFunc("/3/discover/tv", func(w http.ResponseWriter, r *http.Request) {
		last = r.URL.Query()
		j(w, map[string]any{"page": 2, "total_pages": 900, "total_results": 18000,
			"results": []map[string]any{{"id": 1399, "name": "Trónok harca", "first_air_date": "2011-04-17", "poster_path": "/got.jpg", "vote_average": 8.4}}})
	})
	mux.HandleFunc("/3/movie/603/watch/providers", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"results": map[string]any{
			"US": map[string]any{"flatrate": []map[string]any{{"provider_id": 15, "provider_name": "Hulu"}}},
			"HU": map[string]any{"link": "https://www.themoviedb.org/movie/603/watch?locale=HU",
				"flatrate": []map[string]any{{"provider_id": 1899, "provider_name": "HBO Max", "display_priority": 3}, {"provider_id": 8, "provider_name": "Netflix", "display_priority": 1}},
				"ads":      []map[string]any{{"provider_id": 8, "provider_name": "Netflix"}},
				"rent":     []map[string]any{{"provider_id": 2, "provider_name": "Apple TV"}}},
		}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSetting("tmdb_key", "k123")
	st.SetSetting("metadata_lang", "hu-HU")
	f := New(st, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.BaseURL, f.ImageURL, f.Delay = ts.URL+"/3", "https://img/", 0
	ctx := context.Background()

	if r := f.WatchRegion(); r != "HU" {
		t.Errorf("region from metadata language: %q", r)
	}
	// the country's own order, logos at w92, cached
	ps, err := f.WatchProviders(ctx, "movie")
	if err != nil || len(ps) != 3 || ps[0].Name != "Netflix" || ps[1].Name != "HBO Max" || ps[0].Logo != "https://img/w92/nf.jpg" {
		t.Fatalf("providers: %v %+v", err, ps)
	}
	f.WatchProviders(ctx, "movie")
	if provHits.Load() != 1 {
		t.Errorf("provider list not cached: %d calls", provHits.Load())
	}

	p, err := f.Browse(ctx, BrowseFilter{Kind: "series", Providers: []int{8, 1899, 0}, Genres: []int{18}, Sort: "top", YearFrom: 2010, YearTo: 2019, MinRating: 7.5, Lang: "HU", Country: "hu", Page: 2})
	if err != nil || len(p.Results) != 1 || p.Results[0].Title != "Trónok harca" || p.Results[0].Year != 2011 || p.Results[0].Poster != "https://img/w342/got.jpg" ||
		p.TotalPages != 500 || p.Total != 18000 {
		t.Fatalf("browse: %v %+v", err, p)
	}
	for k, want := range map[string]string{"with_watch_providers": "8|1899", "watch_region": "HU", "with_watch_monetization_types": "flatrate|free|ads",
		"with_genres": "18", "sort_by": "vote_average.desc", "vote_count.gte": "300", "first_air_date.gte": "2010-01-01",
		"first_air_date.lte": "2019-12-31", "vote_average.gte": "7.5", "with_original_language": "hu", "with_origin_country": "HU", "page": "2", "include_adult": "false"} {
		if got := last.Get(k); got != want {
			t.Errorf("browse %s = %q, want %q", k, got, want)
		}
	}
	if _, err := f.Browse(ctx, BrowseFilter{Kind: "series", Country: "HU", NoCountry: true}); err != nil {
		t.Fatal(err)
	}
	if oc := "|" + last.Get("with_origin_country") + "|"; strings.Contains(oc, "|HU|") || !strings.Contains(oc, "|US|") || !strings.Contains(oc, "|SU|") {
		t.Errorf("excluding a country: %q", oc)
	}

	mux.HandleFunc("/3/tv/1399/videos", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_video_language") != "hu,en,null" {
			t.Errorf("trailer languages: %q", r.URL.RawQuery)
		}
		j(w, map[string]any{"results": []map[string]any{
			{"key": "clip", "site": "YouTube", "type": "Clip", "iso_639_1": "hu"},
			{"key": "vimeo", "site": "Vimeo", "type": "Trailer", "iso_639_1": "hu", "official": true},
			{"key": "en1", "site": "YouTube", "type": "Trailer", "iso_639_1": "en", "official": true},
			{"key": "hu1", "site": "YouTube", "type": "Teaser", "iso_639_1": "hu"},
		}})
	})
	if tr, err := f.TitleTrailer(ctx, "series", 1399); err != nil || tr.Key != "hu1" {
		t.Errorf("trailer: %v %+v", err, tr)
	}

	if _, err := f.Browse(ctx, BrowseFilter{Sort: "random"}); err == nil {
		t.Error("unknown sort accepted")
	}

	w, err := f.TitleProviders(ctx, "movie", 603)
	if err != nil || w.Region != "HU" || w.Link == "" || len(w.Stream) != 2 || w.Stream[0].Name != "Netflix" || len(w.Rent) != 1 || len(w.Buy) != 0 {
		t.Fatalf("where to watch: %v %+v", err, w)
	}

	// an explicit region wins over the metadata language
	st.SetSetting("watch_region", "de")
	if r := f.WatchRegion(); r != "DE" {
		t.Errorf("region setting: %q", r)
	}
	if w, _ := f.TitleProviders(ctx, "movie", 603); len(w.Stream) != 0 || w.Region != "DE" {
		t.Errorf("no German offer expected: %+v", w)
	}
}

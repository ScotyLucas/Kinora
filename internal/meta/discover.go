package meta

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Felfedezés browsing: TMDB /discover with streaming-service, genre, year and rating
// filters, the streaming services of the server's region, and where a title can be watched.

// WatchProvider is a streaming service (Netflix, HBO Max, …) as TMDB lists it.
type WatchProvider struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Logo string `json:"logo,omitempty"`
}

// Genre is a TMDB genre in the metadata language.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// BrowseFilter narrows a /discover listing. Zero values mean "no filter".
type BrowseFilter struct {
	Kind      string // "movie" or "series"
	Providers []int  // any of these streaming services (subscription, free or with ads)
	Genres    []int  // all of these genres
	Sort      string // popular (default), new, top
	YearFrom  int    // first release / first air year
	YearTo    int
	MinRating float64 // TMDB vote average
	Lang      string  // original language, ISO 639-1 ("hu")
	Country   string  // country of origin, ISO 3166-1 ("HU")
	NoCountry bool    // leave Country out instead of keeping only it
	Page      int
}

// BrowsePage is one page of a /discover listing.
type BrowsePage struct {
	Results    []SearchResult `json:"results"`
	Page       int            `json:"page"`
	TotalPages int            `json:"total_pages"`
	Total      int            `json:"total"`
}

// WhereToWatch lists the services that carry a title in the server's region.
type WhereToWatch struct {
	Region string          `json:"region"`
	Link   string          `json:"link,omitempty"` // the TMDB page that links out to each service
	Stream []WatchProvider `json:"stream"`         // subscription, free, with ads
	Rent   []WatchProvider `json:"rent"`
	Buy    []WatchProvider `json:"buy"`
}

// WatchRegion is the country the streaming catalogues are read for: the region part of
// the metadata language ("hu-HU" → "HU"), or US.
func (f *Fetcher) WatchRegion() string {
	if r := strings.TrimSpace(f.st.Setting("watch_region")); len(r) == 2 {
		return strings.ToUpper(r)
	}
	if lang := f.st.Setting("metadata_lang"); len(lang) == 5 && lang[2] == '-' {
		return strings.ToUpper(lang[3:])
	}
	return "US"
}

type cached struct {
	at  time.Time
	val any
}

const listTTL = 24 * time.Hour

func (f *Fetcher) cacheGet(key string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cache[key]
	if !ok || time.Since(c.at) > listTTL {
		return nil, false
	}
	return c.val, true
}

func (f *Fetcher) cachePut(key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		f.cache = map[string]cached{}
	}
	f.cache[key] = cached{time.Now(), v}
}

func tmdbKind(kind string) string {
	if kind == "series" || kind == "tv" {
		return "tv"
	}
	return "movie"
}

type providerJSON struct {
	ProviderID      int    `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	LogoPath        string `json:"logo_path"`
	DisplayPriority int    `json:"display_priority"`
	// /watch/providers/{kind} carries the priority per country
	DisplayPriorities map[string]int `json:"display_priorities"`
}

func (f *Fetcher) provider(p providerJSON) WatchProvider {
	w := WatchProvider{ID: p.ProviderID, Name: p.ProviderName}
	if p.LogoPath != "" {
		w.Logo = f.ImageURL + "w92" + p.LogoPath
	}
	return w
}

// WatchProviders lists the streaming services of the server's region for films or series,
// most prominent first (cached for a day).
func (f *Fetcher) WatchProviders(ctx context.Context, kind string) ([]WatchProvider, error) {
	k, region := tmdbKind(kind), f.WatchRegion()
	key := "prov/" + k + "/" + region
	if v, ok := f.cacheGet(key); ok {
		return v.([]WatchProvider), nil
	}
	var r struct {
		Results []providerJSON `json:"results"`
	}
	if err := f.get(ctx, "/watch/providers/"+k, map[string][]string{"watch_region": {region}}, &r); err != nil {
		return nil, err
	}
	prio := func(p providerJSON) int {
		if n, ok := p.DisplayPriorities[region]; ok {
			return n
		}
		return p.DisplayPriority
	}
	sort.SliceStable(r.Results, func(i, j int) bool { return prio(r.Results[i]) < prio(r.Results[j]) })
	out := []WatchProvider{}
	for _, p := range r.Results {
		out = append(out, f.provider(p))
	}
	f.cachePut(key, out)
	return out, nil
}

// Genres lists the TMDB genres for films or series (cached for a day).
func (f *Fetcher) Genres(ctx context.Context, kind string) ([]Genre, error) {
	k := tmdbKind(kind)
	key := "genre/" + k + "/" + f.lang(ctx)
	if v, ok := f.cacheGet(key); ok {
		return v.([]Genre), nil
	}
	var r struct {
		Genres []Genre `json:"genres"`
	}
	if err := f.get(ctx, "/genre/"+k+"/list", nil, &r); err != nil {
		return nil, err
	}
	if r.Genres == nil {
		r.Genres = []Genre{}
	}
	f.cachePut(key, r.Genres)
	return r.Genres, nil
}

func joinInts(ids []int, sep string) string {
	s := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			s = append(s, strconv.Itoa(id))
		}
	}
	return strings.Join(s, sep)
}

// Browse returns one page of TMDB /discover with the filter applied.
func (f *Fetcher) Browse(ctx context.Context, b BrowseFilter) (BrowsePage, error) {
	k := tmdbKind(b.Kind)
	date := "primary_release_date"
	if k == "tv" {
		date = "first_air_date"
	}
	q := map[string][]string{"include_adult": {"false"}}
	set := func(k, v string) { q[k] = []string{v} }
	switch b.Sort {
	case "", "popular":
		set("sort_by", "popularity.desc")
	case "new":
		set("sort_by", date+".desc")
		set(date+".lte", time.Now().Format("2006-01-02")) // not the announced-for-2031 ones
		set("vote_count.gte", "10")                       // nor the empty stubs
	case "top":
		set("sort_by", "vote_average.desc")
		set("vote_count.gte", "300")
	default:
		return BrowsePage{}, errors.New("ismeretlen rendezés")
	}
	if s := joinInts(b.Providers, "|"); s != "" {
		set("with_watch_providers", s)
		set("watch_region", f.WatchRegion())
		set("with_watch_monetization_types", "flatrate|free|ads")
	}
	if s := joinInts(b.Genres, ","); s != "" {
		set("with_genres", s)
	}
	if b.YearFrom > 0 {
		set(date+".gte", strconv.Itoa(b.YearFrom)+"-01-01")
	}
	if b.YearTo > 0 {
		set(date+".lte", strconv.Itoa(b.YearTo)+"-12-31")
	}
	if b.MinRating > 0 {
		set("vote_average.gte", strconv.FormatFloat(b.MinRating, 'f', 1, 64))
		if q["vote_count.gte"] == nil {
			set("vote_count.gte", "50") // a single 10/10 vote is not a rating
		}
	}
	if len(b.Lang) == 2 {
		set("with_original_language", strings.ToLower(b.Lang))
	}
	if c := strings.ToUpper(b.Country); len(c) == 2 {
		if b.NoCountry {
			// TMDB has no without_origin_country: ask for every other country instead (| means "any of").
			others := make([]string, 0, len(isoCountries))
			for _, o := range isoCountries {
				if o != c {
					others = append(others, o)
				}
			}
			set("with_origin_country", strings.Join(others, "|"))
		} else {
			set("with_origin_country", c)
		}
	}
	page := b.Page
	if page < 1 {
		page = 1
	}
	if page > 500 { // TMDB's hard limit
		page = 500
	}
	set("page", strconv.Itoa(page))
	var r struct {
		searchResp
		Page         int `json:"page"`
		TotalPages   int `json:"total_pages"`
		TotalResults int `json:"total_results"`
	}
	if err := f.get(ctx, "/discover/"+k, q, &r); err != nil {
		return BrowsePage{}, err
	}
	out := BrowsePage{Results: []SearchResult{}, Page: r.Page, TotalPages: min(r.TotalPages, 500), Total: r.TotalResults}
	for _, x := range r.Results {
		sr := SearchResult{ID: x.ID, Title: x.Title, Year: yearOf(x.ReleaseDate), Overview: x.Overview, Rating: x.VoteAverage}
		if k == "tv" {
			sr.Title, sr.Year = x.Name, yearOf(x.FirstAirDate)
		}
		if x.PosterPath != "" {
			sr.Poster = f.ImageURL + "w342" + x.PosterPath
		}
		out.Results = append(out.Results, sr)
	}
	return out, nil
}

// TitleProviders tells where a film or series can be watched in the server's region.
func (f *Fetcher) TitleProviders(ctx context.Context, kind string, id int64) (WhereToWatch, error) {
	region := f.WatchRegion()
	var r struct {
		Results map[string]struct {
			Link     string         `json:"link"`
			Flatrate []providerJSON `json:"flatrate"`
			Free     []providerJSON `json:"free"`
			Ads      []providerJSON `json:"ads"`
			Rent     []providerJSON `json:"rent"`
			Buy      []providerJSON `json:"buy"`
		} `json:"results"`
	}
	if err := f.get(ctx, "/"+tmdbKind(kind)+"/"+strconv.FormatInt(id, 10)+"/watch/providers", nil, &r); err != nil {
		return WhereToWatch{}, err
	}
	w := WhereToWatch{Region: region, Stream: []WatchProvider{}, Rent: []WatchProvider{}, Buy: []WatchProvider{}}
	c, ok := r.Results[region]
	if !ok {
		return w, nil
	}
	w.Link = c.Link
	list := func(groups ...[]providerJSON) []WatchProvider {
		seen := map[int]bool{}
		out := []WatchProvider{}
		for _, g := range groups {
			sort.SliceStable(g, func(i, j int) bool { return g[i].DisplayPriority < g[j].DisplayPriority })
			for _, p := range g {
				if !seen[p.ProviderID] {
					seen[p.ProviderID] = true
					out = append(out, f.provider(p))
				}
			}
		}
		return out
	}
	w.Stream, w.Rent, w.Buy = list(c.Flatrate, c.Free, c.Ads), list(c.Rent), list(c.Buy)
	return w, nil
}

// isoCountries: ISO 3166-1 codes, plus the former countries TMDB still uses for old titles (SU, CS, XC, XG, YU).
var isoCountries = strings.Fields(`AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS XK YE YT ZA ZM ZW SU CS XC XG YU`)

// Trailer is a title's YouTube trailer; Key is the YouTube video id ("" when TMDB knows none).
type Trailer struct {
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

// TitleTrailer picks a YouTube trailer for a film or series: one in the metadata language first, then
// English; official trailers before teasers and fan uploads.
func (f *Fetcher) TitleTrailer(ctx context.Context, kind string, id int64) (Trailer, error) {
	lang := f.lang(ctx)
	short := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	var r struct {
		Results []struct {
			Key      string `json:"key"`
			Name     string `json:"name"`
			Site     string `json:"site"`
			Type     string `json:"type"`
			Official bool   `json:"official"`
			Lang     string `json:"iso_639_1"`
		} `json:"results"`
	}
	q := url.Values{"include_video_language": {short + ",en,null"}}
	if err := f.get(ctx, "/"+tmdbKind(kind)+"/"+strconv.FormatInt(id, 10)+"/videos", q, &r); err != nil {
		return Trailer{}, err
	}
	best, score := Trailer{}, -1
	for _, v := range r.Results {
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		s := 0
		switch v.Type {
		case "Trailer":
			s += 4
		case "Teaser":
			s += 2
		default:
			continue
		}
		if v.Official {
			s++
		}
		if v.Lang == short {
			s += 8
		}
		if s > score {
			best, score = Trailer{Key: v.Key, Name: v.Name}, s
		}
	}
	return best, nil
}

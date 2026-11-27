package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ScotyLucas/kinora/internal/acquire"
	"github.com/ScotyLucas/kinora/internal/meta"
	"github.com/ScotyLucas/kinora/internal/store"
)

// Felfedezés and requests (acquisition M2): anyone with an account may ask for a film,
// except from a kids profile; an admin approves (or requests are approved automatically).

// viewCtx is r's context with the viewer's metadata language, so Felfedezés shows TMDB titles, overviews
// and genres in the client's UI language (X-Kinora-Lang). Requests themselves keep metadata_lang.
func viewCtx(r *http.Request, c *reqCtx) context.Context {
	if c.Profile == nil {
		return r.Context()
	}
	return meta.WithLang(r.Context(), c.Profile.MetaLang)
}

func requester(c *reqCtx) acquire.Requester {
	r := acquire.Requester{UserID: c.User.ID, IsAdmin: c.User.IsAdmin && c.ProfileLock == 0}
	if c.Profile != nil {
		r.ProfileID, r.Kids = c.Profile.ID, c.Profile.IsKids
	}
	return r
}

// acqUser returns the manager when requests are possible for this viewer, else answers the request.
func (s *Server) acqUser(w http.ResponseWriter, c *reqCtx) *acquire.Manager {
	m := s.acq(w)
	if m == nil {
		return nil
	}
	if !m.Enabled() {
		writeErr(w, http.StatusConflict, acquire.ErrDisabled.Error())
		return nil
	}
	if c.Profile != nil && c.Profile.IsKids {
		writeErr(w, http.StatusForbidden, acquire.ErrKids.Error())
		return nil
	}
	return m
}

func acqErrStatus(err error) int {
	switch {
	case errors.Is(err, acquire.ErrDisabled), errors.Is(err, acquire.ErrHave), errors.Is(err, acquire.ErrRequestBusy), errors.Is(err, acquire.ErrQuota):
		return http.StatusConflict
	case errors.Is(err, acquire.ErrKids), errors.Is(err, acquire.ErrNotYours):
		return http.StatusForbidden
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, meta.ErrNoKey):
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	list := r.URL.Query().Get("list")
	_, movie := meta.MovieLists[list]
	_, tv := meta.TVLists[list]
	if !movie && !tv {
		writeErr(w, http.StatusBadRequest, "ismeretlen lista")
		return
	}
	res, err := m.Discover(viewCtx(r, c), list)
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	m.MarkWatched(profileID(c), res)
	writeJSON(w, http.StatusOK, res)
}

// ints parses a comma list of positive ids ("8,337"), at most max of them.
func ints(v string, max int) []int {
	out := []int{}
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 && len(out) < max {
			out = append(out, n)
		}
	}
	return out
}

// handleDiscoverBrowse: a filtered listing (streaming services, genres, years, rating).
func (s *Server) handleDiscoverBrowse(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	q := r.URL.Query()
	b := meta.BrowseFilter{Kind: "movie", Providers: ints(q.Get("providers"), 20), Genres: ints(q.Get("genres"), 5),
		Sort: q.Get("sort"), Lang: q.Get("lang")}
	if q.Get("kind") == "series" {
		b.Kind = "series"
	}
	b.YearFrom, _ = strconv.Atoi(q.Get("from"))
	b.YearTo, _ = strconv.Atoi(q.Get("to"))
	b.MinRating, _ = strconv.ParseFloat(q.Get("rating"), 64)
	b.Page, _ = strconv.Atoi(q.Get("page"))
	b.Country, b.NoCountry = strings.CutPrefix(q.Get("country"), "!") // "!HU": everything but Hungarian titles
	switch b.Sort {
	case "", "popular", "new", "top":
	default:
		writeErr(w, http.StatusBadRequest, "ismeretlen rendezés")
		return
	}
	res, err := m.Browse(viewCtx(r, c), b)
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	m.MarkWatched(profileID(c), res.Results)
	writeJSON(w, http.StatusOK, res)
}

// handleDiscoverOptions: the streaming services of the region and the genres.
func (s *Server) handleDiscoverOptions(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	o, err := m.Options(viewCtx(r, c))
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handleDiscoverSearch(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	res, err := m.DiscoverSearch(viewCtx(r, c), q, r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	m.MarkWatched(profileID(c), res)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDiscoverMovie(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	d, err := m.DiscoverMovie(viewCtx(r, c), pathID(r, "id"))
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	d.Watched = m.WatchedTitle(profileID(c), "movie", d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleRequestCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	var in struct {
		Kind      string `json:"kind"` // movie (default) | series
		TmdbID    int64  `json:"tmdb_id"`
		Seasons   string `json:"seasons"`    // series: "all", "future" or "1,2"
		LibraryID int64  `json:"library_id"` // films library for a film, series library for a series
	}
	if !decode(w, r, &in) {
		return
	}
	if in.TmdbID <= 0 {
		writeErr(w, http.StatusBadRequest, "hiányzik a tmdb_id")
		return
	}
	var q *store.AcqRequest
	var err error
	if in.Kind == "series" {
		q, err = m.RequestSeries(r.Context(), requester(c), in.TmdbID, in.Seasons, in.LibraryID)
	} else {
		q, err = m.Request(r.Context(), requester(c), in.TmdbID, in.LibraryID)
	}
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// handleDiscoverLibraries lists where a request can go: films libraries for a film, series
// libraries for a series (mixed libraries are not offered).
func (s *Server) handleDiscoverLibraries(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if s.acqUser(w, c) == nil {
		return
	}
	type lib struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := map[string][]lib{"movies": {}, "shows": {}}
	libs, _ := s.visibleLibraries(c.Profile)
	for _, l := range libs {
		if l.Kind == "movies" || l.Kind == "shows" {
			out[l.Kind] = append(out[l.Kind], lib{l.ID, l.Name})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDiscoverSeries(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acqUser(w, c)
	if m == nil {
		return
	}
	d, err := m.DiscoverSeries(viewCtx(r, c), pathID(r, "id"))
	if err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	d.Watched = m.WatchedTitle(profileID(c), "series", d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleRequestsMine(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	list, err := s.st.AcqRequests(c.User.ID, 200)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRequestCancel(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	m := s.acq(w)
	if m == nil {
		return
	}
	if err := m.CancelRequest(pathID(r, "id"), requester(c)); err != nil {
		writeErr(w, acqErrStatus(err), err.Error())
		return
	}
	ok(w)
}

// handleRequestsUnseen returns the user's fulfilled requests they were not told about, and marks them told.
func (s *Server) handleRequestsUnseen(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	list, err := s.st.UnseenAvailable(c.User.ID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	if len(list) > 0 {
		s.st.MarkRequestsSeen(c.User.ID)
	}
	writeJSON(w, http.StatusOK, list)
}

// ------------------------------------------------------------------ admin

func (s *Server) handleAdminRequests(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var states []string
	if r.URL.Query().Get("status") == "open" {
		states = store.OpenRequestStates
	}
	list, err := s.st.AcqRequests(0, queryInt(r, "limit", 200), states...)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAdminRequestDecide(approve bool) handler {
	return func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		m := s.acq(w)
		if m == nil {
			return
		}
		var in struct {
			Note string `json:"note"`
		}
		if r.ContentLength > 0 && !decode(w, r, &in) {
			return
		}
		if err := m.Decide(pathID(r, "id"), c.User.ID, approve, strings.TrimSpace(in.Note)); err != nil {
			code := acqErrStatus(err)
			if code == http.StatusBadGateway {
				code = http.StatusConflict
			}
			writeErr(w, code, err.Error())
			return
		}
		ok(w)
	}
}

// acqRequestSettings are the request settings of PUT /api/admin/acquire/settings.
func (s *Server) saveRequestSettings(w http.ResponseWriter, auto *string, quota, retry *int) bool {
	if auto != nil {
		if *auto != "all" && *auto != "admins" {
			writeErr(w, http.StatusBadRequest, "az automatikus jóváhagyás értéke all vagy admins lehet")
			return false
		}
		s.st.SetSetting("acq_auto_approve", *auto)
	}
	if quota != nil {
		if *quota < 0 || *quota > 1000 {
			writeErr(w, http.StatusBadRequest, "a heti kvóta 0–1000 lehet (0 = nincs korlát)")
			return false
		}
		s.st.SetSetting("acq_quota_week", strconv.Itoa(*quota))
	}
	if retry != nil {
		if *retry < 1 || *retry > 168 {
			writeErr(w, http.StatusBadRequest, "az újrakeresés 1–168 óra lehet")
			return false
		}
		s.st.SetSetting("acq_retry_hours", strconv.Itoa(*retry))
	}
	return true
}

// profileID is the active profile of the request (0 if none).
func profileID(c *reqCtx) int64 {
	if c.Profile == nil {
		return 0
	}
	return c.Profile.ID
}

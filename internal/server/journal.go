package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ScotyLucas/kinora/internal/store"
)

// handleJournal: GET /api/journal?from=&to= (unix seconds, to exclusive). The client groups the
// viewings itself (by its own time zone), so this returns the raw events of the range.
// Default range: the current calendar year.
func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	q := r.URL.Query()
	y := time.Now().Year()
	from := time.Date(y, 1, 1, 0, 0, 0, 0, time.Local).Unix()
	to := time.Date(y+1, 1, 1, 0, 0, 0, 0, time.Local).Unix()
	if v, err := strconv.ParseInt(q.Get("from"), 10, 64); err == nil {
		from = v
	}
	if v, err := strconv.ParseInt(q.Get("to"), 10, 64); err == nil {
		to = v
	}
	if to <= from {
		writeErr(w, http.StatusBadRequest, "érvénytelen időszak")
		return
	}
	evs, err := s.st.Journal(c.Profile.ID, from, to)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	if s.Meta != nil {
		s.Meta.KickCredits() // the breakdowns fill in as the credits arrive
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "events": evs})
}

var recapMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// handleJournalRecapSeen: POST /api/journal/recap-seen {"month":"YYYY-MM"}. The home page's monthly
// recap card stays closed for that month on every device of the profile (kids' profiles included).
func (s *Server) handleJournalRecapSeen(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Month string `json:"month"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !recapMonth.MatchString(in.Month) {
		writeErr(w, http.StatusBadRequest, "érvénytelen hónap")
		return
	}
	if err := s.st.SetJournalRecapSeen(c.Profile.ID, in.Month); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// ------------------------------------------------------------------ Napló 2

// tmdbReady answers with an error when TMDB is not set up.
func (s *Server) tmdbReady(w http.ResponseWriter) bool {
	if s.Meta == nil || !s.Meta.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "ehhez TMDB-kulcs kell (Admin → Metaadatok)")
		return false
	}
	return true
}

func journalErr(s *Server, w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotManual), errors.Is(err, store.ErrTooMany):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrDuplicate):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		s.internalErr(w, r, err)
	}
}

// handleJournalRating: PUT /api/journal/events/{id}/rating {"rating":0..5}
func (s *Server) handleJournalRating(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Rating int `json:"rating"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Rating < 0 || in.Rating > 5 {
		writeErr(w, http.StatusBadRequest, "az értékelés 0 és 5 között lehet")
		return
	}
	if err := s.st.SetEventRating(c.Profile.ID, pathID(r, "id"), in.Rating); err != nil {
		journalErr(s, w, r, err)
		return
	}
	ok(w)
}

// handleJournalAdd: POST /api/journal/events {"tmdb_id":…, "at":unix, "rating":0..5} — a film seen
// outside Kinora. The title, year, runtime, genres and poster come from TMDB.
func (s *Server) handleJournalAdd(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		TmdbID int64 `json:"tmdb_id"`
		At     int64 `json:"at"`
		Rating int   `json:"rating"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.TmdbID <= 0 || in.Rating < 0 || in.Rating > 5 {
		writeErr(w, http.StatusBadRequest, "hiányzó vagy érvénytelen adat")
		return
	}
	// not in the future (a day of leeway for time zones), not before the cinema
	if in.At == 0 {
		in.At = time.Now().Unix()
	}
	if in.At > time.Now().Add(24*time.Hour).Unix() || in.At < time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).Unix() {
		writeErr(w, http.StatusBadRequest, "érvénytelen időpont")
		return
	}
	if !s.tmdbReady(w) {
		return
	}
	m, err := s.Meta.Movie(viewCtx(r, c), in.TmdbID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	id, err := s.st.AddManualEvent(c.Profile.ID, store.ManualEvent{TmdbID: m.ID, Title: m.Title, Year: m.Year, RuntimeSec: m.Runtime * 60,
		Genres: m.Genres, Poster: m.Poster, At: in.At, Rating: in.Rating, ItemID: s.st.LibraryMovieByTmdb(c.Profile, m.ID)})
	if err != nil {
		journalErr(s, w, r, err)
		return
	}
	s.Meta.KickCredits()
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleJournalDelete: DELETE /api/journal/events/{id} — only a viewing added by hand.
func (s *Server) handleJournalDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.st.DeleteEvent(c.Profile.ID, pathID(r, "id")); err != nil {
		journalErr(s, w, r, err)
		return
	}
	ok(w)
}

// handleJournalSearch: GET /api/journal/search?q=&type=movie|series|person|collection — TMDB search for
// adding a viewing by hand and for building challenges (no acquisition rights needed).
func (s *Server) handleJournalSearch(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	if !s.tmdbReady(w) {
		return
	}
	ctx := viewCtx(r, c)
	var res any
	var err error
	switch r.URL.Query().Get("type") {
	case "person":
		res, err = s.Meta.SearchPeople(ctx, q)
	case "collection":
		res, err = s.Meta.SearchCollections(ctx, q)
	case "series":
		res, err = s.Meta.Search(ctx, "series", q, 0)
	default:
		res, err = s.Meta.Search(ctx, "movie", q, 0)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleJournalAbandoned: GET /api/journal/abandoned — titles started and left early.
func (s *Server) handleJournalAbandoned(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	items, err := s.st.Abandoned(c.Profile)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleJournalAbandonedHide: POST /api/journal/abandoned/{id}/hide — put a film or series away from the list.
func (s *Server) handleJournalAbandonedHide(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	it, err := s.st.Item(pathID(r, "id"), c.Profile)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	id := it.ID
	if it.SeriesID != nil {
		id = *it.SeriesID
	}
	if err := s.st.HideAbandoned(c.Profile.ID, id); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handleJournalGoals: GET /api/journal/goals
func (s *Server) handleJournalGoals(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	gs, err := s.st.JournalGoals(c.Profile.ID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

// handleJournalGoalAdd: POST /api/journal/goals {"period":"year|month","metric":"films|hours|episodes|genre","genre":"","target":n}
func (s *Server) handleJournalGoalAdd(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var g store.JournalGoal
	if !decode(w, r, &g) {
		return
	}
	g.Genre = strings.TrimSpace(g.Genre)
	valid := (g.Period == "year" || g.Period == "month") && g.Target >= 1 && g.Target <= 10000
	switch g.Metric {
	case "films", "hours", "episodes":
		g.Genre = ""
	case "genre":
		valid = valid && g.Genre != "" && len(g.Genre) <= 80
	default:
		valid = false
	}
	if !valid {
		writeErr(w, http.StatusBadRequest, "érvénytelen cél")
		return
	}
	id, err := s.st.AddJournalGoal(c.Profile.ID, g)
	if err != nil {
		journalErr(s, w, r, err)
		return
	}
	g.ID = id
	writeJSON(w, http.StatusOK, g)
}

// handleJournalGoalDelete: DELETE /api/journal/goals/{id}
func (s *Server) handleJournalGoalDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.st.DeleteJournalGoal(c.Profile.ID, pathID(r, "id")); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handleJournalChallenges: GET /api/journal/challenges
func (s *Server) handleJournalChallenges(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	cs, err := s.st.Challenges(c.Profile)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

// challengeIn is the body of a new challenge or of added titles: titles by TMDB id (from a search),
// or a person's films (person_id, role "directing" or "acting") or a collection (collection_id) to fill it from.
type challengeIn struct {
	Title        string                `json:"title"`
	Items        []store.ChallengeItem `json:"items"`
	PersonID     int64                 `json:"person_id"`
	Role         string                `json:"role"`
	CollectionID int64                 `json:"collection_id"`
}

// fill resolves the person or collection into titles; it returns the name to use as a default title.
func (s *Server) fillChallenge(w http.ResponseWriter, r *http.Request, c *reqCtx, in *challengeIn) bool {
	if in.PersonID > 0 || in.CollectionID > 0 {
		if !s.tmdbReady(w) {
			return false
		}
		var name string
		var items []store.ChallengeItem
		var err error
		if in.PersonID > 0 {
			name, items, err = s.Meta.PersonFilms(viewCtx(r, c), in.PersonID, in.Role)
		} else {
			name, items, err = s.Meta.CollectionFilms(viewCtx(r, c), in.CollectionID)
		}
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "nem található a TMDB-ben")
			return false
		} else if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return false
		}
		if strings.TrimSpace(in.Title) == "" {
			in.Title = name
		}
		in.Items = append(in.Items, items...)
	}
	clean := in.Items[:0]
	for _, it := range in.Items {
		it.Title = strings.TrimSpace(it.Title)
		if (it.Kind == "movie" || it.Kind == "series") && it.TmdbID > 0 && it.Title != "" && len(it.Title) <= 300 && len(it.Poster) <= 500 &&
			(it.Poster == "" || strings.HasPrefix(it.Poster, "https://image.tmdb.org/")) {
			clean = append(clean, it)
		}
	}
	in.Items = clean
	return true
}

// handleJournalChallengeAdd: POST /api/journal/challenges (see challengeIn)
func (s *Server) handleJournalChallengeAdd(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in challengeIn
	if !decode(w, r, &in) || !s.fillChallenge(w, r, c, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 120 {
		writeErr(w, http.StatusBadRequest, "adj nevet a kihívásnak")
		return
	}
	id, err := s.st.AddChallenge(c.Profile.ID, in.Title, in.Items)
	if err != nil {
		journalErr(s, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleJournalChallengeItems: POST /api/journal/challenges/{id}/items (see challengeIn; the title is ignored)
func (s *Server) handleJournalChallengeItems(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in challengeIn
	if !decode(w, r, &in) || !s.fillChallenge(w, r, c, &in) {
		return
	}
	if err := s.st.AddChallengeItems(c.Profile.ID, pathID(r, "id"), in.Items); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handleJournalChallengeEdit: PATCH /api/journal/challenges/{id} {"title":…}
func (s *Server) handleJournalChallengeEdit(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 120 {
		writeErr(w, http.StatusBadRequest, "adj nevet a kihívásnak")
		return
	}
	if err := s.st.RenameChallenge(c.Profile.ID, pathID(r, "id"), in.Title); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handleJournalChallengeDelete: DELETE /api/journal/challenges/{id}
func (s *Server) handleJournalChallengeDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.st.DeleteChallenge(c.Profile.ID, pathID(r, "id")); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handleJournalChallengeItemDelete: DELETE /api/journal/challenges/{id}/items/{kind}/{tmdb}
func (s *Server) handleJournalChallengeItemDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	kind := r.PathValue("kind")
	if kind != "movie" && kind != "series" {
		writeErr(w, http.StatusBadRequest, "érvénytelen típus")
		return
	}
	if err := s.st.RemoveChallengeItem(c.Profile.ID, pathID(r, "id"), kind, pathID(r, "tmdb")); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

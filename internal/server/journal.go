package server

import (
	"net/http"
	"regexp"
	"strconv"
	"time"
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

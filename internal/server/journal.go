package server

import (
	"net/http"
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

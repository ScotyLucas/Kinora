package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ScotyLucas/kinora/internal/store"
)

// GET /api/items/{id}/trickplay – the timeline preview sheets of an item. While they are
// not made yet the answer is {"ready":false} and the item moves to the front of the queue.
func (s *Server) handleTrickplay(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := pathID(r, "id")
	if !s.st.ItemVisible(id, c.Profile) {
		writeErr(w, http.StatusNotFound, "az elem nem található")
		return
	}
	var m any
	if s.Trickplay != nil {
		if mf := s.Trickplay.Manifest(id); mf != nil {
			m = mf
		}
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	if m == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true, "manifest": m})
}

// GET /api/items/{id}/trickplay/{sheet} – one sheet, e.g. 0.jpg.
func (s *Server) handleTrickplaySheet(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := pathID(r, "id")
	if c.ViaStreamToken && c.StreamItem != 0 && c.StreamItem != id {
		writeErr(w, http.StatusForbidden, "a token nem erre az elemre szól")
		return
	}
	filter, ok := s.imageFilter(c)
	if !ok || (filter != nil && !s.st.ItemVisible(id, filter)) {
		writeErr(w, http.StatusNotFound, "nem található")
		return
	}
	n, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("sheet"), ".jpg"))
	p := ""
	if err == nil && s.Trickplay != nil {
		p = s.Trickplay.SheetPath(id, n)
	}
	if p == "" {
		writeErr(w, http.StatusNotFound, "nincs előnézeti kép")
		return
	}
	// the manifest's sig changes with the file, and clients put it in the URL (?v=), so a sheet can be cached
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, p)
}

// imageFilter is the profile that decides what artwork and pictures a request may see:
// the chosen profile, the locked one, or (on the profile picker) the account's libraries.
// ok is false when a locked profile no longer exists.
func (s *Server) imageFilter(c *reqCtx) (*store.Profile, bool) {
	filter := c.Profile
	if filter == nil && c.ProfileLock > 0 {
		filter, _ = s.st.Profile(c.ProfileLock)
		if filter == nil {
			return nil, false
		}
	}
	if filter == nil && c.User != nil && c.User.Libraries != nil {
		filter = &store.Profile{UserID: c.User.ID, Libraries: c.User.Libraries}
	}
	return filter, true
}

// GET /api/admin/trickplay
func (s *Server) handleTrickplayStatus(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if s.Trickplay == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, s.Trickplay.Status())
}

package server

import (
	"context"
	"net/http"
	"time"
)

// Ratings from other sites and the box office, loaded by the detail page and the Felfedezés dialog
// after they render (meta.TitleScores).

const scoresTimeout = 8 * time.Second

// handleItemScores: GET /api/items/{id}/scores – a film or series in the library.
func (s *Server) handleItemScores(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	it, err := s.st.ItemFull(pathID(r, "id"), c.Profile) // also applies the profile's rating limit
	if err != nil {
		writeErr(w, http.StatusNotFound, "az elem nem található")
		return
	}
	if it.Kind != "movie" && it.Kind != "series" {
		writeErr(w, http.StatusBadRequest, "csak filmhez és sorozathoz van értékelés")
		return
	}
	var tmdb int64
	if it.TmdbID != nil {
		tmdb = *it.TmdbID
	}
	ctx, cancel := context.WithTimeout(r.Context(), scoresTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, s.Meta.TitleScores(ctx, it.Kind, tmdb, it.ImdbID))
}

// handleDiscoverScores: GET /api/discover/{movies|series}/{id}/scores – a TMDB title.
func (s *Server) handleDiscoverScores(kind string) func(http.ResponseWriter, *http.Request, *reqCtx) {
	return func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		if s.acqUser(w, c) == nil { // same audience as the dialog itself
			return
		}
		id := pathID(r, "id")
		if id <= 0 {
			writeErr(w, http.StatusBadRequest, "hibás azonosító")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scoresTimeout)
		defer cancel()
		writeJSON(w, http.StatusOK, s.Meta.TitleScores(ctx, kind, id, ""))
	}
}

// handleDiscoverTrailer: the YouTube trailer of a film or series in the Felfedezés ({"key": ""} when none).
func (s *Server) handleDiscoverTrailer(kind string) func(http.ResponseWriter, *http.Request, *reqCtx) {
	return func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		if s.acqUser(w, c) == nil {
			return
		}
		id := pathID(r, "id")
		if id <= 0 {
			writeErr(w, http.StatusBadRequest, "hibás azonosító")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), scoresTimeout)
		defer cancel()
		t, err := s.Meta.TitleTrailer(viewCtx(r.WithContext(ctx), c), kind, id)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, t)
	}
}

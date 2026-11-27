package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ScotyLucas/kinora/internal/scanner"
)

// handleImage serves artwork with a fallback chain so cards are never empty
// when *something* exists: backdrop → thumb, season → series, series → first episode.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := pathID(r, "id")
	kind := r.PathValue("kind")
	if kind != "poster" && kind != "backdrop" && kind != "thumb" {
		writeErr(w, http.StatusNotFound, "ismeretlen képtípus")
		return
	}
	if c.ViaStreamToken && c.StreamItem != 0 && c.StreamItem != id {
		writeErr(w, http.StatusForbidden, "a token nem erre az elemre szól")
		return
	}
	filter, ok := s.imageFilter(c)
	if !ok || (filter != nil && !s.st.ItemVisible(id, filter)) {
		writeErr(w, http.StatusNotFound, "nem található")
		return
	}
	k, seriesID := s.st.ItemKindSeries(id)
	file := func(iid int64, name string) string {
		p := filepath.Join(scanner.ImageDir(s.ImgDir, iid), name+".jpg")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	var chain []func() string
	own := func(n string) func() string { return func() string { return file(id, n) } }
	series := func(n string) func() string {
		return func() string {
			if seriesID > 0 && seriesID != id {
				return file(seriesID, n)
			}
			return ""
		}
	}
	firstEp := func() string {
		if k == "series" || k == "season" {
			if eid := s.st.FirstEpisode(id); eid > 0 {
				return file(eid, "thumb")
			}
		}
		return ""
	}
	switch kind {
	case "poster":
		chain = []func() string{own("poster"), series("poster")}
	case "backdrop":
		chain = []func() string{own("backdrop"), own("thumb"), series("backdrop"), firstEp}
	case "thumb":
		chain = []func() string{own("thumb"), own("backdrop"), firstEp, series("backdrop")}
	}
	for _, f := range chain {
		if p := f(); p != "" {
			w.Header().Set("Cache-Control", "private, max-age=3600")
			w.Header().Set("Content-Type", "image/jpeg")
			http.ServeFile(w, r, p)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeErr(w, http.StatusNotFound, "nincs kép")
}

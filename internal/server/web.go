package server

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/ScotyLucas/kinora/web"
)

// webFS is the web client's files: embedded, or from --web for development.
func (s *Server) webFS() fs.FS {
	if s.cfg.WebDir != "" {
		return os.DirFS(s.cfg.WebDir)
	}
	sub, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// webHandler serves the single page app (embedded, or from --web for development).
func (s *Server) webHandler() http.Handler {
	root := s.webFS()
	if s.cfg.WebDir != "" {
		s.log.Info("serving web client from disk", "dir", s.cfg.WebDir)
	}
	files := http.FileServer(http.FS(root))
	dev := s.cfg.WebDir != ""
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			// SPA uses hash routing; unknown paths get the shell.
			p = "index.html"
			r.URL.Path = "/"
		}
		if p == "index.html" {
			// the shell carries the server's link-preview tags (preview.go)
			page, err := s.indexWithMeta(root, r)
			if err != nil {
				http.Error(w, "a webes felület nem érhető el", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(page)
			return
		}
		switch {
		case dev:
			w.Header().Set("Cache-Control", "no-cache")
		case strings.HasPrefix(p, "vendor/"):
			w.Header().Set("Cache-Control", "public, max-age=604800")
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

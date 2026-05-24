package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// handleSPA serves the embedded single-page app. Real files are served as-is;
// unknown paths fall back to index.html so client-side routing works.
// API routes never reach here — the mux matches "/api/v2/..." first.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if s.fileServer == nil {
		http.Error(w, "web UI not built", http.StatusNotFound)
		return
	}

	clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if clean == "" {
		clean = "index.html"
	}

	// If the requested file does not exist, serve index.html (SPA fallback).
	if _, err := fs.Stat(s.webFS, clean); err != nil {
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}
	s.fileServer.ServeHTTP(w, r)
}

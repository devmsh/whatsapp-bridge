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
		clean = "index.html"
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}

	// Cache policy:
	//   • index.html / any HTML — never cache. Vite hashes asset filenames, so
	//     a fresh index.html is the ONLY way the browser sees the new bundle.
	//     Without this, browsers heuristic-cache HTML for days and the user
	//     keeps loading an old JS bundle.
	//   • /assets/* — already hashed, safe to cache forever.
	if strings.HasSuffix(clean, ".html") || clean == "index.html" {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
	} else if strings.HasPrefix(clean, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	s.fileServer.ServeHTTP(w, r)
}

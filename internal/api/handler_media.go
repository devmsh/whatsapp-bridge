package api

import (
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	// /api/v2/media/{subpath...}
	subPath := strings.TrimPrefix(r.URL.Path, "/api/v2/media/")
	if subPath == "" {
		jsonError(w, 400, "path required")
		return
	}

	// Validate that the path doesn't escape the media directory
	clean := filepath.Clean(subPath)
	if strings.Contains(clean, "..") {
		jsonError(w, 400, "invalid path")
		return
	}

	fullPath := filepath.Join(s.mediaDir, clean)
	http.ServeFile(w, r, fullPath)
}

package api

import (
	"net/http"
	"path/filepath"
	"strings"
)

// handleMedia streams a file stored under the bridge's media directory.
//
// Hidden-chats privacy is enforced here too: the URL alone identifies a file,
// but every file we serve was downloaded by a specific message — so we look
// up the owning chat JID via the messages table and run it through
// guardChatAccess. Without this check, anyone with a media URL could fetch a
// file from a locked DM without unlocking it. Files not referenced by any
// message (e.g. a stale upload) are denied to be safe.
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

	// Validate that the path doesn't escape the media directory.
	clean := filepath.Clean(subPath)
	if strings.Contains(clean, "..") {
		jsonError(w, 400, "invalid path")
		return
	}

	// The messages table stores media_path with a "store/" prefix (the bridge's
	// data dir). Reconstruct that form and look up the owning chat.
	storedPath := "store/" + clean
	owner := s.store.ChatJIDForMediaPath(storedPath)
	if owner == "" {
		// No message references this file — refuse rather than leak orphans.
		jsonError(w, 404, "not found")
		return
	}
	if !s.guardChatAccess(w, r, owner) {
		return
	}

	fullPath := filepath.Join(s.mediaDir, clean)
	http.ServeFile(w, r, fullPath)
}

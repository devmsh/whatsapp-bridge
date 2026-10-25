package api

import (
	"net/http"
	"runtime/debug"
)

// handleCapabilities reports the pinned whatsmeow version and which bridge
// features are available. Username lookup is both stored-data
// (username_stored_lookup) and a live usync query (username_live_lookup).
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jsonOK(w, map[string]any{
		"whatsmeow_version": whatsmeowVersion(),
		"features": map[string]bool{
			"username_stored_lookup": true,
			"username_live_lookup":   true,
			"newsletters":            true,
			"polls":                  true,
			"reactions":              true,
			"edits":                  true,
			"lid_mapping":            true,
			"groups":                 true,
			"presence":               true,
			"history_sync":           true,
			"media":                  true,
			"calls":                  true,
			"app_state":              true,
		},
	})
}

func whatsmeowVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range bi.Deps {
		if d.Path == "go.mau.fi/whatsmeow" {
			if d.Version != "" {
				return d.Version
			}
			return "unknown"
		}
	}
	return "unknown"
}

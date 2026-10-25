package api

import (
	"net/http"
	"runtime/debug"
)

// handleCapabilities reports the pinned whatsmeow version and which bridge
// features are available. Username lookup is stored-data only: we return
// handles WhatsApp has already sent us, and we do not live-query WhatsApp
// for a username we have never seen.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jsonOK(w, map[string]any{
		"whatsmeow_version": whatsmeowVersion(),
		"features": map[string]bool{
			"username_stored_lookup": true,
			"username_live_lookup":   false,
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

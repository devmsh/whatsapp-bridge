package api

import (
	"context"
	"fmt"
	"net/http"

	"whatsapp-bridge-v2/internal/wa"
)

// handleSettingsMedia gets or updates the media auto-download policy.
// GET  /api/v2/settings/media -> current policy
// PUT  /api/v2/settings/media -> update + persist (applies to new messages)
func (s *Server) handleSettingsMedia(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, s.client.MediaPolicy())
	case http.MethodPut:
		var p wa.MediaPolicy
		if err := decodeJSON(r, &p); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if err := s.client.SetMediaPolicy(p); err != nil {
			jsonError(w, 500, fmt.Sprintf("save policy: %v", err))
			return
		}
		jsonOK(w, s.client.MediaPolicy())
	default:
		methodNotAllowed(w)
	}
}

// handleSettingsHistory gets or sets the history sync period.
// GET /api/v2/settings/history -> {period, options}
// PUT /api/v2/settings/history -> set period; takes effect on next link.
//
// If currently logged out, we restart the QR flow so the new period is sent
// with the next pairing.
func (s *Server) handleSettingsHistory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, map[string]interface{}{
			"period":  s.client.HistoryPeriod(),
			"options": []string{wa.History3Months, wa.History1Year, wa.HistoryEverything},
			"note":    "Takes effect at the next link (unlink + scan again).",
		})
	case http.MethodPut:
		var req struct {
			Period string `json:"period"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if !wa.ValidHistoryPeriod(req.Period) {
			jsonError(w, 400, "invalid period (use 3months, 1year, or everything)")
			return
		}
		if err := s.client.SetHistoryPeriod(req.Period); err != nil {
			jsonError(w, 500, fmt.Sprintf("save period: %v", err))
			return
		}
		// Not yet linked? Restart the QR flow so the new period applies now.
		applied := false
		if s.client.Auth.Snapshot().State != wa.StateConnected {
			if err := s.client.Auth.StartLogin(context.Background()); err == nil {
				applied = true
			}
		}
		jsonOK(w, map[string]interface{}{
			"period":      s.client.HistoryPeriod(),
			"applied_now": applied,
			"note":        "If already linked, unlink and scan again for this to take effect.",
		})
	default:
		methodNotAllowed(w)
	}
}

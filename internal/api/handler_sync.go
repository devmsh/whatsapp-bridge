package api

import (
	"fmt"
	"net/http"
	"strings"

	"whatsapp-bridge-v2/internal/wa"
)

func (s *Server) handleSyncContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := wa.SyncContacts(s.client); err != nil {
		jsonError(w, 500, fmt.Sprintf("sync contacts: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handleSyncHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	waClient := s.client.GetWhatsmeowClient()
	if !waClient.IsConnected() {
		jsonError(w, 503, "not connected")
		return
	}

	// History sync happens automatically on reconnection
	// This endpoint triggers a groups sync as a useful alternative
	if err := wa.SyncGroups(s.client); err != nil {
		jsonError(w, 500, fmt.Sprintf("sync failed: %v", err))
		return
	}

	jsonOK(w, map[string]string{"status": "sync complete"})
}

func (s *Server) handleSyncState(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/v2/sync/state/")
	if key == "" {
		jsonError(w, 400, "key required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		value, updatedAt, err := s.store.GetSyncState(key)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if value == "" && updatedAt == 0 {
			jsonError(w, 404, "key not found")
			return
		}
		jsonOK(w, map[string]interface{}{
			"key":        key,
			"value":      value,
			"updated_at": updatedAt,
		})
	case http.MethodPost:
		var req struct {
			Value string `json:"value"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if err := s.store.PutSyncState(key, req.Value); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]string{"status": "ok"})
	default:
		methodNotAllowed(w)
	}
}

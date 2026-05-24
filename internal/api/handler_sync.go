package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/wa"
)

// handleSyncProgress reports onboarding sync progress: whatsmeow's history-sync
// signals plus live DB counts so the GUI can show numbers growing.
// GET /api/v2/sync/progress
func (s *Server) handleSyncProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	var msgCount, chatCount, contactCount int
	s.store.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&msgCount)
	s.store.DB.QueryRow("SELECT COUNT(*) FROM chats").Scan(&chatCount)
	s.store.DB.QueryRow("SELECT COUNT(*) FROM contacts").Scan(&contactCount)

	progress := s.client.Sync.Snapshot()
	connected := s.client.IsConnected()

	// "receiving" = a history batch arrived in the last 15s. This is the honest
	// signal: WhatsApp pushes history in bursts, so quiet means "settled for now",
	// not necessarily "all history downloaded" (more may arrive later).
	receiving := progress.LastBatchAt > 0 && time.Now().Unix()-progress.LastBatchAt < 15

	phase := "idle"
	switch {
	case !connected:
		phase = "offline"
	case receiving:
		phase = "receiving"
	case progress.HistoryBatches == 0 && !progress.InitialSyncDone:
		phase = "starting"
	}

	jsonOK(w, map[string]interface{}{
		"connected": connected,
		"receiving": receiving,
		"phase":     phase,
		"progress":  progress,
		"counts": map[string]int{
			"messages": msgCount,
			"chats":    chatCount,
			"contacts": contactCount,
		},
	})
}

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

func (s *Server) handleSyncMigrateLID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	count, err := wa.MigrateLIDMessages(s.client)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("migration failed: %v", err))
		return
	}
	jsonOK(w, map[string]interface{}{
		"status":   "ok",
		"migrated": count,
	})
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

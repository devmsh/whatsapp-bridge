package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"

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

// handleSyncAppStateReplay forces whatsmeow to re-replay every app-state
// collection with fullSync=true. Used to retroactively pick up DeleteForMe /
// DeleteChat events that arrived before this bridge had a handler for them,
// plus any other app-state mutations (pins/mutes/archives).
//
// POST /api/v2/sync/app-state-replay
func (s *Server) handleSyncAppStateReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	waClient := s.client.GetWhatsmeowClient()
	if waClient == nil || !waClient.IsConnected() {
		jsonError(w, 503, "not connected")
		return
	}

	// Make sure events are emitted while we re-fetch — by default whatsmeow
	// suppresses some app-state events on full sync.
	waClient.EmitAppStateEventsOnFullSync = true

	// Kick off the replay in the background so the HTTP call returns fast.
	go func() {
		ctx := context.Background()
		for _, name := range appstate.AllPatchNames {
			if err := waClient.FetchAppState(ctx, name, true, false); err != nil {
				s.client.Log.Errorf("app-state replay (%s) failed: %v", name, err)
				continue
			}
			s.client.Log.Infof("app-state replay (%s) complete", name)
		}
	}()
	jsonOK(w, map[string]any{"status": "app-state replay started"})
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

// handleSyncAppStateRecover repairs a corrupted app-state collection.
//
// Why this exists: `regular_low` carries pin and archive state. Ours had never
// synced — every fetch died on "failed to verify patch: mismatching LTHash",
// so is_pinned was 0 for every chat and the chat list could not reproduce
// WhatsApp's order, where pinned chats sit on top. A replay does not help: the
// snapshot itself fails verification, so there is nothing to re-apply.
//
// The fix is the recovery path WhatsApp itself uses. We ask the primary device
// (the phone) for an unencrypted copy of the collection. whatsmeow handles the
// reply in handleAppStateRecovery and applies the snapshot directly, skipping
// the broken hash chain. The phone must be online to answer.
//
// POST /api/v2/sync/app-state-recover   body: {"collection":"regular_low"}
func (s *Server) handleSyncAppStateRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	waClient := s.client.GetWhatsmeowClient()
	if waClient == nil || !waClient.IsConnected() {
		jsonError(w, 503, "not connected")
		return
	}

	var req struct {
		Collection string `json:"collection"`
	}
	_ = decodeJSON(r, &req)
	name := appstate.WAPatchName(strings.TrimSpace(req.Collection))
	if name == "" {
		name = appstate.WAPatchRegularLow
	}
	known := false
	for _, n := range appstate.AllPatchNames {
		if n == name {
			known = true
			break
		}
	}
	if !known {
		jsonError(w, 400, "unknown collection: "+string(name))
		return
	}

	// The reply is handled as a full sync, and whatsmeow stays silent on full
	// syncs unless this is set. Without it the snapshot lands inside whatsmeow
	// but no Pin/Archive/Mute event ever reaches our handlers, so the chats
	// table keeps its old values and nothing visibly changes.
	waClient.EmitAppStateEventsOnFullSync = true

	// Wipe the local state of the collection first, so the snapshot the phone
	// sends is not judged "not newer than what we have" and thrown away, and so
	// leftover mutation MACs do not collide with the ones it carries.
	//
	// It must be a wipe, not a write of version 0: whatsmeow rejects a saved
	// version of 0 outright ("invalid saved app state version 0") and drops the
	// recovery reply before applying it.
	if err := s.client.ResetAppStateCollection(string(name)); err != nil {
		jsonError(w, 500, fmt.Sprintf("could not reset %s: %v", name, err))
		return
	}

	resp, err := waClient.SendPeerMessage(r.Context(), whatsmeow.BuildAppStateRecoveryRequest(name))
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("recovery request failed: %v", err))
		return
	}
	jsonOK(w, map[string]any{
		"status":     "recovery requested",
		"collection": string(name),
		"request_id": resp.ID,
		"note":       "the phone answers out of band; re-check pinned/archived state in a few seconds",
	})
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

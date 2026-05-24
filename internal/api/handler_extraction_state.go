package api

import (
	"net/http"
	"strings"
	"time"
)

// handleExtractionMark advances the per-chat watermark used by incremental
// task extraction. The MCP tool wa_mark_extracted calls this — the MCP server
// itself opens SQLite read-only, so all writes go through the REST API.
//
// POST /api/v2/extractions/mark  {"chat_jid","session_id"}
//   -> {"chat_jid","watermark","session_id"}
func (s *Server) handleExtractionMark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID   string `json:"chat_jid"`
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ChatJID) == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}

	// Watermark = the chat's current max(timestamp). Empty chats record a 0
	// watermark so the next run still skips them quickly.
	var maxTS int64
	s.store.DB.QueryRow(`SELECT COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid = ?`, req.ChatJID).Scan(&maxTS)
	now := time.Now().Unix()
	if _, err := s.store.DB.Exec(`INSERT INTO chat_extraction_state
		(chat_jid, last_msg_ts, last_session_id, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			last_msg_ts = excluded.last_msg_ts,
			last_session_id = excluded.last_session_id,
			updated_at = excluded.updated_at`,
		req.ChatJID, maxTS, req.SessionID, now); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"chat_jid":   req.ChatJID,
		"watermark":  maxTS,
		"session_id": req.SessionID,
	})
}

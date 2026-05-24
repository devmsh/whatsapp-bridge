package api

import "net/http"

// handleStatsMessages returns the message count and last-activity time per chat.
// Used by the UI to sort contacts/chats by how much you talk to them.
// GET /api/v2/stats/messages
func (s *Server) handleStatsMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rows, err := s.store.DB.Query(
		`SELECT chat_jid, COUNT(*) AS c, MAX(timestamp) AS last FROM messages GROUP BY chat_jid`,
	)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type stat struct {
		ChatJID       string `json:"chat_jid"`
		Count         int    `json:"count"`
		LastMessageAt int64  `json:"last_message_at"`
	}
	out := []stat{}
	for rows.Next() {
		var st stat
		if err := rows.Scan(&st.ChatJID, &st.Count, &st.LastMessageAt); err == nil {
			out = append(out, st)
		}
	}
	jsonOK(w, out)
}

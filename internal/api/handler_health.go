package api

import (
	"net/http"
)

const version = "2.0.0"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	connected := s.client.IsConnected()
	uptime := s.client.Uptime().Seconds()

	// Get DB stats
	var msgCount, chatCount, contactCount int
	s.store.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&msgCount)
	s.store.DB.QueryRow("SELECT COUNT(*) FROM chats").Scan(&chatCount)
	s.store.DB.QueryRow("SELECT COUNT(*) FROM contacts").Scan(&contactCount)

	jsonOK(w, map[string]interface{}{
		"connected": connected,
		"uptime":    uptime,
		"version":   version,
		"db_stats": map[string]int{
			"messages": msgCount,
			"chats":    chatCount,
			"contacts": contactCount,
		},
	})
}

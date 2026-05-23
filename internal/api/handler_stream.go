package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleStream implements GET /api/v2/stream
// Query params:
//   - jid — filter to a specific chat JID (optional, default: all chats)
//
// Response: text/event-stream — one SSE event per incoming message.
// Each event is: data: <json>\n\n
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	filterJID := r.URL.Query().Get("jid")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send a comment to confirm the stream opened
	fmt.Fprintf(w, ": connected jid=%q\n\n", filterJID)
	flusher.Flush()

	ch := s.client.Broadcaster.Subscribe(filterJID)
	defer s.client.Broadcaster.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

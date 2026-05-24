package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// handleAuthStatus returns the current login state (one-shot).
// GET /api/v2/auth/status
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jsonOK(w, s.client.Auth.Snapshot())
}

// handleAuthStream pushes login-state changes as Server-Sent Events.
// GET /api/v2/auth/stream
func (s *Server) handleAuthStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.client.Auth.Subscribe()
	defer s.client.Auth.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(state)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// handleAuthLogin (re)starts the login flow. Useful after a logout to get a
// fresh QR, or to retry after an error.
// POST /api/v2/auth/login
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := s.client.Auth.StartLogin(context.Background()); err != nil {
		jsonError(w, 500, fmt.Sprintf("start login: %v", err))
		return
	}
	jsonOK(w, s.client.Auth.Snapshot())
}

// handleAuthLogout unlinks the device. State resets to a fresh QR.
// POST /api/v2/auth/logout
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := s.client.Auth.Logout(context.Background()); err != nil {
		jsonError(w, 500, fmt.Sprintf("logout: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

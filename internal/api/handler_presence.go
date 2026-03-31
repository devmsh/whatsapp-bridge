package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func (s *Server) handlePresenceSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	err := wa.SendPresence(context.Background(), types.Presence(req.State))
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set presence: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handlePresenceTyping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID   string `json:"jid"`
		State string `json:"state"`
		Media string `json:"media,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	jid, err := parseJID(req.JID)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	err = wa.SendChatPresence(context.Background(), jid, types.ChatPresence(req.State), types.ChatPresenceMedia(req.Media))
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("send typing: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handlePresenceSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JID string `json:"jid"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	jid, err := parseJID(req.JID)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	err = wa.SubscribePresence(context.Background(), jid)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("subscribe: %v", err))
		return
	}
	jsonOK(w, map[string]bool{"success": true})
}

func (s *Server) handlePresenceGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jid := strings.TrimPrefix(r.URL.Path, "/api/v2/presence/")
	entry, err := s.store.GetPresence(jid)
	if err != nil {
		jsonError(w, 404, "presence not found")
		return
	}
	jsonOK(w, entry)
}

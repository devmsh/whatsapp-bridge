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

// handlePresenceSubscribeBulk subscribes to presence for many chats at once.
// POST /api/v2/presence/subscribe-bulk  {"jids": ["...", ...]}
//
// WhatsApp only pushes typing/online for peers you have subscribed to, and
// subscriptions are per-peer. Subscribing only to the OPEN chat is why the
// chat list could never show "typing…" for a chat you had not opened.
//
// Failures are counted, not fatal: a peer that blocks presence, or a group,
// simply will not report — that is normal and must not fail the batch.
func (s *Server) handlePresenceSubscribeBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		JIDs []string `json:"jids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	// Cap the batch: with 250+ chats an unbounded loop would hammer the
	// socket on every reconnect for no benefit — only visible rows matter.
	const maxBulk = 128
	if len(req.JIDs) > maxBulk {
		req.JIDs = req.JIDs[:maxBulk]
	}

	wa := s.client.GetWhatsmeowClient()
	ok, failed := 0, 0
	for _, raw := range req.JIDs {
		// parseJID accepts a bare word as a user with an empty server, so
		// check for a server part explicitly — otherwise garbage counts as
		// a success and the failed tally lies.
		if !strings.Contains(raw, "@") {
			failed++
			continue
		}
		jid, err := parseJID(raw)
		if err != nil {
			failed++
			continue
		}
		if err := wa.SubscribePresence(context.Background(), jid); err != nil {
			failed++
			continue
		}
		ok++
	}
	jsonOK(w, map[string]any{"subscribed": ok, "failed": failed})
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

// handleTypingSnapshot returns every chat that currently has at least one
// fresh 'composing' beacon — group typing cache + DM presence rows where
// status='composing' within the freshness window. The chat list polls this
// once every few seconds to render WA's "typing…" preview without N+1.
//
// Shape:
//
//	{ "chats": { "<chatJID>": ["<senderJID>", ...], ... } }
//
// For DMs the sender list is just [chatJID] (the peer themselves) — kept
// uniform with groups so the client treats both the same way.
func (s *Server) handleTypingSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	out := map[string][]string{}
	// Groups: in-memory beacons.
	for chat, typers := range s.client.Typing.Snapshot() {
		out[chat] = typers
	}
	// DMs: presence_cache rows where the peer is actively composing. Same
	// 10s freshness as the in-memory cache so both surfaces age out together.
	if composers, err := s.store.ActiveComposers(10); err == nil {
		for _, jid := range composers {
			// Don't clobber a group entry that happens to share the JID
			// (shouldn't be possible — DMs and groups have different
			// suffixes — but defensive).
			if _, exists := out[jid]; !exists {
				out[jid] = []string{jid}
			}
		}
	}
	// Voice notes are a separate state in WhatsApp ("recording audio…"), so
	// report them alongside rather than folding them into "typing…".
	audio := map[string][]string{}
	for chat, senders := range s.client.Typing.SnapshotAudio() {
		audio[chat] = senders
	}
	if recorders, err := s.store.ActiveRecorders(10); err == nil {
		for _, jid := range recorders {
			if _, exists := audio[jid]; !exists {
				audio[jid] = []string{jid}
			}
		}
	}
	jsonOK(w, map[string]any{"chats": out, "audio": audio})
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

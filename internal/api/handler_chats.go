package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	chats, err := s.store.GetChats()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	previews, _ := s.store.GetChatPreviews()
	// "Private mode": when the request is unlocked, the chat list shows ONLY
	// hidden chats (replacing the normal list). When locked, the normal list
	// is returned with hidden chats excluded. The two never mix — same model
	// as WhatsApp's locked-chats secret code.
	hidden := s.store.HiddenChatJIDs()
	// Dual-identity expansion: hidden_chats stores rows by whatever JID form
	// the user hid (usually phone @s.whatsapp.net), but the same conversation
	// can be stored in `chats` under its @lid alternate. Without this, a
	// phone-form hidden chat leaks into the archive (or main list) under its
	// LID form. Walk the hidden set once and add every alternate form too.
	for j := range hidden {
		if alt := s.client.ResolveLIDForJID(j); alt != "" {
			hidden[alt] = true
		} else if alt := s.client.ResolvePhoneForLID(j); alt != "" {
			hidden[alt] = true
		}
	}
	unlocked := s.isUnlocked(r)

	// Precompute per-chat unread @-mention counts so the chat list can show
	// a small '@' badge on chats waiting on the current user specifically
	// (matches official WA's mention indicator). Only chats with unread>0
	// are considered, and we cap the per-chat scan window so a chat with a
	// huge offline backlog doesn't slow the response.
	unreadByJID := map[string]int{}
	for _, c := range chats {
		if c.UnreadCount > 0 && !hidden[c.JID] == !unlocked {
			unreadByJID[c.JID] = c.UnreadCount
		}
	}
	mentionCounts, _ := s.store.UnreadMentionCounts(
		unreadByJID, s.client.SelfMentionPatterns(), 100,
	)

	type chatWithPreview struct {
		db.Chat
		LastMessage     *db.ChatPreview `json:"last_message,omitempty"`
		IsHidden        bool            `json:"is_hidden,omitempty"`
		UnreadMentions  int             `json:"unread_mentions,omitempty"`
	}
	out := make([]chatWithPreview, 0, len(chats))
	for _, c := range chats {
		isHidden := hidden[c.JID]
		// Locked → drop hidden. Unlocked → drop non-hidden.
		if unlocked != isHidden {
			continue
		}
		row := chatWithPreview{Chat: c, IsHidden: isHidden}
		if p, ok := previews[c.JID]; ok {
			pv := p
			row.LastMessage = &pv
		}
		if m := mentionCounts[c.JID]; m > 0 {
			row.UnreadMentions = m
		}
		out = append(out, row)
	}
	jsonOK(w, out)
}

func (s *Server) handleChatByJID(w http.ResponseWriter, r *http.Request) {
	// /api/v2/chats/{jid} or /api/v2/chats/{jid}/action or /api/v2/chats/{jid}/disappearing
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/chats/")
	parts := strings.SplitN(path, "/", 2)
	jid := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "action":
		s.handleChatAction(w, r, jid)
	case "disappearing":
		s.handleChatDisappearing(w, r, jid)
	case "draft-replies":
		s.handleDraftReplies(w, r, jid)
	case "hide":
		s.handleChatHide(w, r, jid)
	case "hide-preview":
		s.handleChatHidePreview(w, r, jid)
	case "unhide":
		s.handleChatUnhide(w, r, jid)
	case "typing":
		s.handleChatTyping(w, r, jid)
	case "events":
		s.handleChatEvents(w, r, jid)
	case "calls":
		s.handleChatCalls(w, r, jid)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if !s.guardChatAccess(w, r, jid) {
			return
		}
		chat, err := s.store.GetChat(jid)
		if err != nil {
			jsonError(w, 404, "chat not found")
			return
		}
		jsonOK(w, chat)
	}
}

func (s *Server) handleChatAction(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Action   string `json:"action"`
		Duration int64  `json:"duration,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	chatJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid jid")
		return
	}

	switch req.Action {
	case "archive":
		wa.SendAppState(context.Background(), appstate.BuildArchive(chatJID, true, time.Now(), nil))
		s.store.SetChatArchived(jid, true)
	case "unarchive":
		wa.SendAppState(context.Background(), appstate.BuildArchive(chatJID, false, time.Now(), nil))
		s.store.SetChatArchived(jid, false)
	case "pin":
		wa.SendAppState(context.Background(), appstate.BuildPin(chatJID, true))
		s.store.SetChatPinned(jid, true)
	case "unpin":
		wa.SendAppState(context.Background(), appstate.BuildPin(chatJID, false))
		s.store.SetChatPinned(jid, false)
	case "mute":
		dur := time.Duration(req.Duration) * time.Hour
		if req.Duration == 0 {
			dur = -1 // forever
		}
		wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, true, dur))
		s.store.SetChatMuted(jid, true, req.Duration)
	case "unmute":
		wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, false, 0))
		s.store.SetChatMuted(jid, false, 0)
	case "read":
		msgs, _ := s.store.GetMessages(jid, 0, 1)
		if len(msgs) > 0 {
			wa.MarkRead(context.Background(), []types.MessageID{msgs[0].ID}, time.Now(), chatJID, chatJID)
		}
		wa.SendAppState(context.Background(), appstate.BuildMarkChatAsRead(chatJID, true, time.Now(), nil))
		s.store.StoreChat(&db.Chat{JID: jid, UnreadCount: 0})
	case "unread":
		wa.SendAppState(context.Background(), appstate.BuildMarkChatAsRead(chatJID, false, time.Now(), nil))
		s.store.StoreChat(&db.Chat{JID: jid, UnreadCount: 1})
	default:
		jsonError(w, 400, "unknown action: "+req.Action)
		return
	}

	jsonOK(w, map[string]bool{"success": true})
}

// handleChatTyping returns the JIDs currently typing in this chat (any
// participant whose 'composing' beacon arrived within the last few seconds —
// see typingFreshSec in wa/typing.go). The chat header polls this every
// few seconds for groups so it can render the WA-style "X is typing…" line.
// Empty array (not 404) when nobody is typing — keeps the client logic
// uniform.
func (s *Server) handleChatTyping(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.guardChatAccess(w, r, jid) {
		return
	}
	typers := s.client.Typing.Typers(jid)
	if typers == nil {
		typers = []string{}
	}
	jsonOK(w, typers)
}

// handleChatCalls returns the call events scoped to this chat — used by the
// timeline to render inline "📞 Voice call" pills the same way WA mobile
// folds calls into the chat. Newest-first, capped by limit (default 200).
func (s *Server) handleChatCalls(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.guardChatAccess(w, r, jid) {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	calls, err := s.store.GetCallsForChat(jid, limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if calls == nil {
		calls = []db.CallEvent{}
	}
	jsonOK(w, calls)
}

// handleChatEvents returns the events_log entries scoped to this chat,
// newest-first. Today only ephemeral_setting (disappearing-timer changes)
// is logged — the client renders each as a centered grey system pill in
// the timeline so the user can see the change in place, exactly how WA
// mobile shows it. Empty array (not 404) when there's nothing to show.
func (s *Server) handleChatEvents(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.guardChatAccess(w, r, jid) {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	events, err := s.store.GetEventLogsForChat(jid, limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if events == nil {
		events = []db.EventLog{}
	}
	jsonOK(w, events)
}

func (s *Server) handleChatDisappearing(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Timer int64 `json:"timer"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	chatJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid jid")
		return
	}

	// Set disappearing timer on WhatsApp
	wa := s.client.GetWhatsmeowClient()
	dur := time.Duration(req.Timer) * time.Second
	if err := wa.SetDisappearingTimer(context.Background(), chatJID, dur, time.Now()); err != nil {
		jsonError(w, 500, "failed to set timer: "+err.Error())
		return
	}

	// Update local DB
	chat, _ := s.store.GetChat(jid)
	if chat != nil {
		chat.DisappearingTimer = req.Timer
		s.store.StoreChat(chat)
	}

	jsonOK(w, map[string]bool{"success": true})
}

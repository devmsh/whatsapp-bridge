package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// HiddenUnlocker mints and validates short-lived tokens that let UI list
// endpoints include hidden chats. AI features ignore this — they never see
// hidden chats regardless of the unlock state.
//
// Two token flavours:
//   - GLOBAL — unlocks the whole "private mode" view (chat list flips to
//     showing only hidden chats, SSE filter swaps, etc).
//   - CHAT-SCOPED — unlocks ONE specific hidden chat for content endpoints
//     (messages, media, send, …). Does NOT affect the chat list or SSE
//     filter, so the rest of the UI stays in normal mode.
//
// Both are 32 hex chars and indistinguishable by shape; the unlocker holds
// the scope server-side.
type HiddenUnlocker struct {
	mu     sync.Mutex
	tokens map[string]tokenInfo // token -> info (empty Chat = global)
}

type tokenInfo struct {
	Chat   string // "" for global; specific JID for chat-scoped
	Expiry int64
}

const hiddenUnlockTTL = 30 * time.Minute

func newHiddenUnlocker() *HiddenUnlocker { return &HiddenUnlocker{tokens: map[string]tokenInfo{}} }

// Mint returns a fresh GLOBAL token good for hiddenUnlockTTL.
func (u *HiddenUnlocker) Mint() string { return u.mint("") }

// MintForChat returns a fresh CHAT-SCOPED token good for hiddenUnlockTTL.
// The token only authorises access to chatJID (and its LID/phone twin, when
// the caller resolves them).
func (u *HiddenUnlocker) MintForChat(chatJID string) string { return u.mint(chatJID) }

func (u *HiddenUnlocker) mint(chat string) string {
	b := make([]byte, 16)
	rand.Read(b)
	t := hex.EncodeToString(b)
	u.mu.Lock()
	u.tokens[t] = tokenInfo{Chat: chat, Expiry: time.Now().Add(hiddenUnlockTTL).Unix()}
	u.mu.Unlock()
	return t
}

// Revoke removes a token (relock). Works for either flavour.
func (u *HiddenUnlocker) Revoke(token string) {
	if token == "" {
		return
	}
	u.mu.Lock()
	delete(u.tokens, token)
	u.mu.Unlock()
}

// Valid reports whether a token is a currently-valid GLOBAL token. A
// chat-scoped token is NOT valid here — those go through ValidForChat. Lazy
// expiry prune.
func (u *HiddenUnlocker) Valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now().Unix()
	u.mu.Lock()
	defer u.mu.Unlock()
	info, ok := u.tokens[token]
	if !ok {
		return false
	}
	if info.Expiry < now {
		delete(u.tokens, token)
		return false
	}
	return info.Chat == "" // only global
}

// ValidForChat reports whether a token authorises access to a specific chat.
// True for any valid GLOBAL token, or for a CHAT-SCOPED token whose JID
// matches one of jidCandidates (caller passes both LID and phone forms).
func (u *HiddenUnlocker) ValidForChat(token string, jidCandidates ...string) bool {
	if token == "" {
		return false
	}
	now := time.Now().Unix()
	u.mu.Lock()
	defer u.mu.Unlock()
	info, ok := u.tokens[token]
	if !ok {
		return false
	}
	if info.Expiry < now {
		delete(u.tokens, token)
		return false
	}
	if info.Chat == "" {
		return true // global token unlocks anything
	}
	for _, j := range jidCandidates {
		if j != "" && j == info.Chat {
			return true
		}
	}
	return false
}

// requestUnlockToken reads the unlock token from either the
// X-Hidden-Unlock header (used by all JSON API calls) or the ?unlock= query
// parameter (used by media <audio>/<img> URLs that can't carry headers).
func requestUnlockToken(r *http.Request) string {
	if t := r.Header.Get("X-Hidden-Unlock"); t != "" {
		return t
	}
	return r.URL.Query().Get("unlock")
}

// isUnlocked reports whether the request has a valid GLOBAL unlock token.
// Used by UI list handlers (chats list, SSE filter) to flip the whole UI
// into private mode. A chat-scoped token does NOT count here — opening one
// hidden chat must not flip the whole UI.
func (s *Server) isUnlocked(r *http.Request) bool {
	if s.hiddenUnlocker == nil {
		return false
	}
	return s.hiddenUnlocker.Valid(requestUnlockToken(r))
}

// guardChatAccess enforces private-mode parity on any endpoint that reads or
// writes content for a specific chat. The rule mirrors the chats list and SSE
// stream: locked mode can only touch non-hidden chats; unlocked mode can only
// touch hidden ones. On denial, writes a 403 JSON response and returns false
// (callers should `return` immediately).
//
// chatJID may be either the phone or LID form of the same conversation — we
// resolve through the whatsmeow client so a LID-form request can't slip past
// a phone-form entry in hidden_chats (or vice versa).
//
// Access to a hidden chat is granted when EITHER:
//   - a GLOBAL unlock token is present (full private mode), OR
//   - a CHAT-SCOPED token for this specific chat is present.
//
// The chat-scoped path is what lets a "click mention → fingerprint" flow open
// one hidden chat without flipping the whole UI.
func (s *Server) guardChatAccess(w http.ResponseWriter, r *http.Request, chatJID string) bool {
	if chatJID == "" {
		return true // upstream returns 400; nothing to guard
	}
	hidden := s.store.HiddenChatJIDs()
	// Collect every JID form this chat may be stored under.
	twin := ""
	if s.client != nil {
		if lid := s.client.ResolveLIDForJID(chatJID); lid != "" {
			twin = lid
		} else if pn := s.client.ResolvePhoneForLID(chatJID); pn != "" {
			twin = pn
		}
	}
	isHidden := hidden[chatJID] || (twin != "" && hidden[twin])

	if !isHidden {
		// Normal chat. Locked mode allows; unlocked (global) mode forbids
		// (symmetric private-mode rule, same as the chat list).
		if s.isUnlocked(r) {
			jsonError(w, 403, "chat is locked")
			return false
		}
		return true
	}

	// Hidden chat. Allowed when EITHER global unlocked OR a per-chat token
	// covers this JID (or its twin).
	if s.hiddenUnlocker == nil {
		jsonError(w, 403, "chat is locked")
		return false
	}
	if s.hiddenUnlocker.ValidForChat(requestUnlockToken(r), chatJID, twin) {
		return true
	}
	jsonError(w, 403, "chat is locked")
	return false
}

// constantTimeEq is a constant-time byte-compare for short strings (PIN hash).
func constantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

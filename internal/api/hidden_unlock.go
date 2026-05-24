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
type HiddenUnlocker struct {
	mu     sync.Mutex
	tokens map[string]int64 // token -> expiry unix
}

const hiddenUnlockTTL = 30 * time.Minute

func newHiddenUnlocker() *HiddenUnlocker { return &HiddenUnlocker{tokens: map[string]int64{}} }

// Mint returns a fresh token good for hiddenUnlockTTL.
func (u *HiddenUnlocker) Mint() string {
	b := make([]byte, 16)
	rand.Read(b)
	t := hex.EncodeToString(b)
	u.mu.Lock()
	u.tokens[t] = time.Now().Add(hiddenUnlockTTL).Unix()
	u.mu.Unlock()
	return t
}

// Revoke removes a token (relock).
func (u *HiddenUnlocker) Revoke(token string) {
	if token == "" {
		return
	}
	u.mu.Lock()
	delete(u.tokens, token)
	u.mu.Unlock()
}

// Valid reports whether a token is currently valid. Also lazily prunes expired.
func (u *HiddenUnlocker) Valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now().Unix()
	u.mu.Lock()
	defer u.mu.Unlock()
	exp, ok := u.tokens[token]
	if !ok {
		return false
	}
	if exp < now {
		delete(u.tokens, token)
		return false
	}
	return true
}

// isUnlocked reads the X-Hidden-Unlock header and reports whether the request
// has a valid unlock token. Used by UI list handlers to opt into showing
// hidden chats. AI features must NOT consult this.
func (s *Server) isUnlocked(r *http.Request) bool {
	if s.hiddenUnlocker == nil {
		return false
	}
	return s.hiddenUnlocker.Valid(r.Header.Get("X-Hidden-Unlock"))
}

// constantTimeEq is a constant-time byte-compare for short strings (PIN hash).
func constantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

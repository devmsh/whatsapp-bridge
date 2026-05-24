package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// PIN is stored as argon2id(pin + salt). The hash+salt live in sync_state.
// When unlocking, we verify PIN, mint a short-lived "pin-passed" handle, and
// require a WebAuthn assertion verified with the same handle before issuing
// the final unlock token.
//
// Argon2id parameters: balanced for a 4-8 digit PIN. Low memory because PINs
// are checked one at a time interactively (no need to resist GPU farms).
const (
	pinKeyHash       = "hidden_pin_hash"
	pinKeySalt       = "hidden_pin_salt"
	pinPassedTTL     = 5 * time.Minute
	argonTime    uint32 = 1
	argonMem     uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
)

// pinPassedTokens tracks "PIN was verified" handles that authorize a WebAuthn
// assertion to mint the final unlock token. Single map shared across handlers.
var pinPassedTokens = struct {
	mu     sync.Mutex
	tokens map[string]int64
}{tokens: map[string]int64{}}

func mintPinPassed() string {
	b := make([]byte, 16)
	rand.Read(b)
	t := hex.EncodeToString(b)
	pinPassedTokens.mu.Lock()
	pinPassedTokens.tokens[t] = time.Now().Add(pinPassedTTL).Unix()
	pinPassedTokens.mu.Unlock()
	return t
}

func validPinPassed(t string) bool {
	if t == "" {
		return false
	}
	pinPassedTokens.mu.Lock()
	defer pinPassedTokens.mu.Unlock()
	exp, ok := pinPassedTokens.tokens[t]
	if !ok || exp < time.Now().Unix() {
		delete(pinPassedTokens.tokens, t)
		return false
	}
	return true
}

func consumePinPassed(t string) bool {
	if t == "" {
		return false
	}
	pinPassedTokens.mu.Lock()
	defer pinPassedTokens.mu.Unlock()
	exp, ok := pinPassedTokens.tokens[t]
	if !ok || exp < time.Now().Unix() {
		delete(pinPassedTokens.tokens, t)
		return false
	}
	delete(pinPassedTokens.tokens, t)
	return true
}

// pinIsSet reports whether the user has configured a PIN.
func (s *Server) pinIsSet() bool {
	v, _, _ := s.store.GetSyncState(pinKeyHash)
	return v != ""
}

func (s *Server) hashPIN(pin string) (hash, salt []byte) {
	salt = make([]byte, 16)
	rand.Read(salt)
	hash = argon2.IDKey([]byte(pin), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	return
}

func (s *Server) verifyPIN(pin string) bool {
	hStr, _, _ := s.store.GetSyncState(pinKeyHash)
	sStr, _, _ := s.store.GetSyncState(pinKeySalt)
	if hStr == "" || sStr == "" {
		return false
	}
	want, err := hex.DecodeString(hStr)
	if err != nil {
		return false
	}
	salt, err := hex.DecodeString(sStr)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	return constantTimeEq(want, got)
}

// handleHiddenStatus reports what setup state the lock is in: pin set,
// webauthn credential registered, currently unlocked.
// GET /api/v2/hidden/status
func (s *Server) handleHiddenStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	credID, _, _ := s.store.GetSyncState(waKeyCredID)
	count := 0
	rows, err := s.store.DB.Query(`SELECT COUNT(*) FROM hidden_chats`)
	if err == nil {
		rows.Next()
		rows.Scan(&count)
		rows.Close()
	}
	jsonOK(w, map[string]any{
		"pin_set":              s.pinIsSet(),
		"webauthn_registered":  credID != "",
		"unlocked":             s.isUnlocked(r),
		"hidden_count":         count,
	})
}

// handleHiddenPinSetup creates or replaces the PIN. The first time it's free;
// after that it requires the current PIN.
// POST /api/v2/hidden/pin/setup  {pin, current_pin?}
func (s *Server) handleHiddenPinSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Pin        string `json:"pin"`
		CurrentPin string `json:"current_pin"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Pin) < 4 || len(req.Pin) > 12 {
		jsonError(w, 400, "pin must be 4-12 characters")
		return
	}
	if s.pinIsSet() {
		if !s.verifyPIN(req.CurrentPin) {
			jsonError(w, 401, "current_pin is wrong")
			return
		}
	}
	hash, salt := s.hashPIN(req.Pin)
	s.store.PutSyncState(pinKeyHash, hex.EncodeToString(hash))
	s.store.PutSyncState(pinKeySalt, hex.EncodeToString(salt))
	jsonOK(w, map[string]any{"pin_set": true})
}

// handleHiddenUnlockPin verifies the PIN and returns a short-lived
// "pin-passed" handle. The caller must follow up with the WebAuthn assertion
// to receive the real unlock token.
// POST /api/v2/hidden/unlock/pin  {pin}  ->  {pin_passed_token}
func (s *Server) handleHiddenUnlockPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct{ Pin string `json:"pin"` }
	if err := decodeJSON(r, &req); err != nil || req.Pin == "" {
		jsonError(w, 400, "pin required")
		return
	}
	if !s.pinIsSet() {
		jsonError(w, 400, "pin not set up")
		return
	}
	// Small constant delay to slow down naive brute force (~150ms total per try).
	time.Sleep(150 * time.Millisecond)
	if !s.verifyPIN(req.Pin) {
		jsonError(w, 401, "wrong pin")
		return
	}
	credID, _, _ := s.store.GetSyncState(waKeyCredID)
	tok := mintPinPassed()
	jsonOK(w, map[string]any{
		"pin_passed_token":   tok,
		"webauthn_registered": credID != "",
	})
}

// handleHiddenLock revokes the current unlock token (relock).
// POST /api/v2/hidden/lock
func (s *Server) handleHiddenLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.hiddenUnlocker.Revoke(r.Header.Get("X-Hidden-Unlock"))
	jsonOK(w, map[string]any{"locked": true})
}

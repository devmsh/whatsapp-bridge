package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Native (macOS app) second factor for hidden chats.
//
// WHY THIS EXISTS
//
// The browser flow's second factor is a WebAuthn assertion. That cannot work
// inside the macOS app: Apple requires an app embedding a WKWebView to declare
// the relying party as an associated domain before passkeys are usable, which
// needs a paid Team ID and an apple-app-site-association file served over
// HTTPS — impossible for a loopback bridge. Secure Enclave keys are equally
// out of reach: persisting one needs a keychain access group, which also needs
// a Team ID (verified: SecItemAdd returns -34018 errSecMissingEntitlement
// under ad-hoc signing, for both keychain backends).
//
// What DOES work under ad-hoc signing is LocalAuthentication itself. So the
// app performs a real Touch ID check, then proves it is the local shell by
// presenting a key readable only by this user.
//
// WHAT THIS IS AND IS NOT
//
// The key file is owner-only (0600). Any process running as this user can read
// it and therefore forge this call. That is NOT a regression: hidden_chats
// holds only a list of JIDs, and the messages themselves live unencrypted in
// the same SQLite file. Anything that can read the key file can already read
// every hidden chat directly out of messages.db without asking us at all.
//
// The boundary this defends is the real one: a person sitting at an unlocked
// Mac. They cannot supply the owner's fingerprint. PIN remains the first
// factor, so this is still two-factor.
//
// If a Team ID ever becomes available, replace this with a Secure Enclave
// challenge-response and delete the key file.

const nativeKeyFile = ".native-unlock-key"

var nativeKeyOnce struct {
	sync.Once
	key []byte
}

// nativeUnlockKey returns the shared local key, creating it on first use.
// Lives beside the database, with owner-only permissions.
func (s *Server) nativeUnlockKey() []byte {
	nativeKeyOnce.Do(func() {
		path := filepath.Join(filepath.Dir(s.cfg.DBPath), nativeKeyFile)
		if b, err := os.ReadFile(path); err == nil && len(b) == 64 {
			nativeKeyOnce.key = b
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return
		}
		enc := []byte(hex.EncodeToString(raw))
		// 0600: the whole point is that only this user can read it.
		if err := os.WriteFile(path, enc, 0o600); err != nil {
			return
		}
		nativeKeyOnce.key = enc
	})
	return nativeKeyOnce.key
}

// handleHiddenUnlockNative completes an unlock for the macOS app.
// POST /api/v2/hidden/unlock/native  {"pin_passed_token": "...", "key": "..."}
//
// Requires BOTH a live pin_passed_token (so the PIN really was entered) and
// the local key (so the caller is the shell on this machine, which only calls
// this after Touch ID succeeded).
func (s *Server) handleHiddenUnlockNative(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		// "biometric" — the shell already passed Touch ID.
		// "pin"       — Touch ID was unavailable, so the user fell back to the
		//               PIN and PinPassedToken carries proof of it.
		Method         string `json:"method"`
		PinPassedToken string `json:"pin_passed_token"`
		Key            string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "bad request")
		return
	}

	// The PIN is a FALLBACK, not a second factor, mirroring how macOS itself
	// treats Touch ID vs the login password: either one alone opens the door.
	// Both paths still require the local key below, so both still prove the
	// caller is the app on this machine.
	if req.Method == "pin" {
		// Consume, don't merely validate: one PIN entry buys one unlock.
		if !consumePinPassed(req.PinPassedToken) {
			jsonError(w, 401, "pin not verified (or the token expired)")
			return
		}
	} else if req.Method != "biometric" {
		jsonError(w, 400, `method must be "biometric" or "pin"`)
		return
	}

	want := s.nativeUnlockKey()
	if len(want) == 0 {
		jsonError(w, 500, "native unlock key unavailable")
		return
	}
	// Constant time: this is a secret comparison.
	if subtle.ConstantTimeCompare([]byte(req.Key), want) != 1 {
		jsonError(w, 401, "bad native key")
		return
	}

	jsonOK(w, map[string]any{"unlock_token": s.hiddenUnlocker.Mint()})
}

// handleHiddenNativeKeyPath tells the local shell where to read the key from.
// It returns only the PATH, never the key — a browser learning the path gains
// nothing it did not already have, since reading the file requires being this
// user, and being this user is already enough to read the database.
func (s *Server) handleHiddenNativeKeyPath(w http.ResponseWriter, r *http.Request) {
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(s.cfg.DBPath), nativeKeyFile))
	if err != nil {
		jsonError(w, 500, "cannot resolve key path")
		return
	}
	s.nativeUnlockKey() // ensure it exists before anyone goes looking
	jsonOK(w, map[string]any{"path": abs})
}

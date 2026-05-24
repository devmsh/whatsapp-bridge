package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthn keys in sync_state.
const (
	waKeyCredID    = "hidden_wa_credential_id"      // hex-encoded raw credential id
	waKeyPubKey    = "hidden_wa_credential_pubkey"  // hex-encoded CBOR public key
	waKeyAAGUID    = "hidden_wa_credential_aaguid"  // hex-encoded AAGUID
	waKeySignCount = "hidden_wa_credential_sign_count"
	waKeyTransports = "hidden_wa_credential_transports"
)

// hiddenWAUser implements webauthn.User. We only ever store one credential
// (single user). Identity is a fixed local name.
type hiddenWAUser struct {
	creds []webauthn.Credential
}

func (u *hiddenWAUser) WebAuthnID() []byte            { return []byte("whatsapp-bridge-user") }
func (u *hiddenWAUser) WebAuthnName() string          { return "you" }
func (u *hiddenWAUser) WebAuthnDisplayName() string   { return "WhatsApp Bridge" }
func (u *hiddenWAUser) WebAuthnIcon() string          { return "" }
func (u *hiddenWAUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// In-memory WebAuthn instance (cheap) + sessionStore for the two-step flows.
var webauthnOnce sync.Once
var webauthnInst *webauthn.WebAuthn
var webauthnInitErr error

// webauthnSessions holds (session_id -> SessionData JSON) for registration
// and assertion. Short TTL.
var webauthnSessions = struct {
	mu   sync.Mutex
	data map[string]waEntry
}{data: map[string]waEntry{}}

type waEntry struct {
	Data    []byte
	Expires int64
}

func putWASession(id string, sd *webauthn.SessionData) {
	b, _ := json.Marshal(sd)
	webauthnSessions.mu.Lock()
	webauthnSessions.data[id] = waEntry{Data: b, Expires: time.Now().Add(5 * time.Minute).Unix()}
	webauthnSessions.mu.Unlock()
}

func popWASession(id string) (*webauthn.SessionData, bool) {
	webauthnSessions.mu.Lock()
	defer webauthnSessions.mu.Unlock()
	e, ok := webauthnSessions.data[id]
	if !ok || e.Expires < time.Now().Unix() {
		delete(webauthnSessions.data, id)
		return nil, false
	}
	delete(webauthnSessions.data, id)
	var sd webauthn.SessionData
	if err := json.Unmarshal(e.Data, &sd); err != nil {
		return nil, false
	}
	return &sd, true
}

// rpForRequest builds a WebAuthn instance tuned to the incoming Origin so the
// browser's relying-party check passes whether the user opens the GUI via
// whatsapp-bridge.test or http://localhost:8082.
func rpForRequest(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = "http://localhost:8082"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	rpID := u.Hostname()
	cfg := &webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "WhatsApp Bridge",
		RPOrigins:     []string{origin, "http://localhost:8082", "http://whatsapp-bridge.test"},
	}
	return webauthn.New(cfg)
}

// storedCredential returns the user's currently registered credential, or
// nil if none.
func (s *Server) storedCredential() *webauthn.Credential {
	idHex, _, _ := s.store.GetSyncState(waKeyCredID)
	pkHex, _, _ := s.store.GetSyncState(waKeyPubKey)
	if idHex == "" || pkHex == "" {
		return nil
	}
	idBytes, _ := hex.DecodeString(idHex)
	pkBytes, _ := hex.DecodeString(pkHex)
	aaguidStr, _, _ := s.store.GetSyncState(waKeyAAGUID)
	aaguid, _ := hex.DecodeString(aaguidStr)
	var count uint32
	if v, _, _ := s.store.GetSyncState(waKeySignCount); v != "" {
		n, _ := strconv.Atoi(v)
		count = uint32(n)
	}
	transportsStr, _, _ := s.store.GetSyncState(waKeyTransports)
	var transports []protocol.AuthenticatorTransport
	if transportsStr != "" {
		for _, t := range strings.Split(transportsStr, ",") {
			if t != "" {
				transports = append(transports, protocol.AuthenticatorTransport(t))
			}
		}
	}
	return &webauthn.Credential{
		ID:        idBytes,
		PublicKey: pkBytes,
		Authenticator: webauthn.Authenticator{
			AAGUID:    aaguid,
			SignCount: count,
		},
		Transport: transports,
	}
}

// handleHiddenWARegisterOptions starts WebAuthn credential registration.
// POST /api/v2/hidden/webauthn/register/options
//   header X-Pin-Passed: token from the pin step
//   returns { publicKey: <PublicKeyCredentialCreationOptions>, session_id }
func (s *Server) handleHiddenWARegisterOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !validPinPassed(r.Header.Get("X-Pin-Passed")) {
		jsonError(w, 401, "pin step required")
		return
	}
	wa, err := rpForRequest(r)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	user := &hiddenWAUser{}
	opts, sd, err := wa.BeginRegistration(user)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	sid := newID()
	putWASession(sid, sd)
	jsonOK(w, map[string]any{"publicKey": opts.Response, "session_id": sid})
}

// handleHiddenWARegisterVerify completes registration.
// POST /api/v2/hidden/webauthn/register/verify
//   header X-Pin-Passed: ...
//   body { session_id, credential }
func (s *Server) handleHiddenWARegisterVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !validPinPassed(r.Header.Get("X-Pin-Passed")) {
		jsonError(w, 401, "pin step required")
		return
	}
	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SessionID == "" {
		jsonError(w, 400, "session_id and credential required")
		return
	}
	sd, ok := popWASession(req.SessionID)
	if !ok {
		jsonError(w, 400, "session expired")
		return
	}
	wa, err := rpForRequest(r)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(req.Credential)))
	if err != nil {
		jsonError(w, 400, "parse: "+err.Error())
		return
	}
	user := &hiddenWAUser{}
	cred, err := wa.CreateCredential(user, *sd, parsed)
	if err != nil {
		jsonError(w, 400, "verify: "+err.Error())
		return
	}
	s.store.PutSyncState(waKeyCredID, fmt.Sprintf("%x", cred.ID))
	s.store.PutSyncState(waKeyPubKey, fmt.Sprintf("%x", cred.PublicKey))
	s.store.PutSyncState(waKeyAAGUID, fmt.Sprintf("%x", cred.Authenticator.AAGUID))
	s.store.PutSyncState(waKeySignCount, fmt.Sprintf("%d", cred.Authenticator.SignCount))
	var tstr []string
	for _, t := range cred.Transport {
		tstr = append(tstr, string(t))
	}
	s.store.PutSyncState(waKeyTransports, strings.Join(tstr, ","))

	jsonOK(w, map[string]any{"registered": true})
}

// handleHiddenWAAuthOptions starts an assertion (unlock).
// POST /api/v2/hidden/webauthn/auth/options
//   header X-Pin-Passed: token from the pin step
func (s *Server) handleHiddenWAAuthOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !validPinPassed(r.Header.Get("X-Pin-Passed")) {
		jsonError(w, 401, "pin step required")
		return
	}
	cred := s.storedCredential()
	if cred == nil {
		jsonError(w, 400, "no webauthn credential registered")
		return
	}
	wa, err := rpForRequest(r)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	user := &hiddenWAUser{creds: []webauthn.Credential{*cred}}
	opts, sd, err := wa.BeginLogin(user)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	sid := newID()
	putWASession(sid, sd)
	jsonOK(w, map[string]any{"publicKey": opts.Response, "session_id": sid})
}

// handleHiddenWAAuthVerify completes the assertion and mints the unlock token.
// POST /api/v2/hidden/webauthn/auth/verify
//   header X-Pin-Passed: token
//   body { session_id, credential }
//   ->  { unlock_token, expires_at }
func (s *Server) handleHiddenWAAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !consumePinPassed(r.Header.Get("X-Pin-Passed")) {
		jsonError(w, 401, "pin step required")
		return
	}
	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SessionID == "" {
		jsonError(w, 400, "session_id and credential required")
		return
	}
	sd, ok := popWASession(req.SessionID)
	if !ok {
		jsonError(w, 400, "session expired")
		return
	}
	cred := s.storedCredential()
	if cred == nil {
		jsonError(w, 400, "no webauthn credential registered")
		return
	}
	wa, err := rpForRequest(r)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(req.Credential)))
	if err != nil {
		jsonError(w, 400, "parse: "+err.Error())
		return
	}
	// macOS Touch ID can flip BackupEligible/BackupState between registration
	// and assertion (iCloud Keychain availability), which go-webauthn rejects
	// as "Backup Eligible flag inconsistency". Sync the stored credential's
	// flags to the current assertion before validation so the check passes.
	authFlags := parsed.Response.AuthenticatorData.Flags
	cred.Flags.BackupEligible = authFlags.HasBackupEligible()
	cred.Flags.BackupState = authFlags.HasBackupState()
	user := &hiddenWAUser{creds: []webauthn.Credential{*cred}}
	got, err := wa.ValidateLogin(user, *sd, parsed)
	if err != nil {
		jsonError(w, 401, "verify: "+err.Error())
		return
	}
	s.store.PutSyncState(waKeySignCount, fmt.Sprintf("%d", got.Authenticator.SignCount))

	tok := s.hiddenUnlocker.Mint()
	jsonOK(w, map[string]any{"unlock_token": tok, "ttl_seconds": int(hiddenUnlockTTL.Seconds())})
}


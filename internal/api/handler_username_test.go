package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/wa"
)

func newUsernameTestServer(t *testing.T) (*Server, *db.Store) {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{store: st, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v2/contacts", s.handleContacts)
	s.mux.HandleFunc("/api/v2/contacts/by-username/", s.handleContactByUsername)
	s.mux.HandleFunc("/api/v2/contacts/lookup-username", s.handleLookupUsername)
	s.mux.HandleFunc("/api/v2/contacts/fetch-usernames", s.handleFetchUsernames)
	s.mux.HandleFunc("/api/v2/capabilities", s.handleCapabilities)
	return s, st
}

func TestHandleContactByUsername(t *testing.T) {
	s, st := newUsernameTestServer(t)
	if err := st.StoreContact(&db.Contact{
		JID:      "966500000101@s.whatsapp.net",
		Name:     "Alice",
		Username: "alice",
	}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}

	cases := []struct {
		path string
		code int
		jid  string
	}{
		{"/api/v2/contacts/by-username/alice", 200, "966500000101@s.whatsapp.net"},
		{"/api/v2/contacts/by-username/Alice", 200, "966500000101@s.whatsapp.net"},
		{"/api/v2/contacts/by-username/@alice", 200, "966500000101@s.whatsapp.net"},
		{"/api/v2/contacts/by-username/nobody", 404, ""},
		{"/api/v2/contacts/by-username/", 400, ""},
		{"/api/v2/contacts/by-username/@", 400, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		s.mux.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%s: status = %d, want %d; body %s", tc.path, rec.Code, tc.code, rec.Body.String())
			continue
		}
		if tc.jid == "" {
			continue
		}
		var got db.Contact
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Errorf("%s: decode: %v", tc.path, err)
			continue
		}
		if got.JID != tc.jid || got.Username != "alice" {
			t.Errorf("%s: got jid=%q username=%q", tc.path, got.JID, got.Username)
		}
	}
}

func TestHandleContactsUsernameQuery(t *testing.T) {
	s, st := newUsernameTestServer(t)
	if err := st.StoreContact(&db.Contact{
		JID: "1@s.whatsapp.net", Name: "Alice", Username: "alice",
	}); err != nil {
		t.Fatalf("store alice: %v", err)
	}
	if err := st.StoreContact(&db.Contact{
		JID: "2@s.whatsapp.net", Name: "Bob", Username: "bobby",
	}); err != nil {
		t.Fatalf("store bob: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/contacts?username=@ALICE", nil)
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var list []db.Contact
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].JID != "1@s.whatsapp.net" {
		t.Fatalf("got %#v", list)
	}
}

func TestHandleCapabilities(t *testing.T) {
	s, _ := newUsernameTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/capabilities", nil)
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		WhatsmeowVersion string          `json:"whatsmeow_version"`
		Features         map[string]bool `json:"features"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WhatsmeowVersion == "" {
		t.Error("whatsmeow_version empty")
	}
	if !got.Features["username_stored_lookup"] {
		t.Error("username_stored_lookup should be true")
	}
	if !got.Features["username_live_lookup"] {
		t.Error("username_live_lookup should be true")
	}
	for _, f := range []string{"newsletters", "polls", "reactions", "edits", "lid_mapping"} {
		if !got.Features[f] {
			t.Errorf("feature %s should be true", f)
		}
	}
}

func TestHandleLookupUsernameValidation(t *testing.T) {
	s, _ := newUsernameTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/contacts/lookup-username", nil)
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/contacts/lookup-username", strings.NewReader(`{"username":""}`))
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("empty username status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/contacts/lookup-username", strings.NewReader(`{"username":"@Alice"}`))
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("no client status = %d, body %s", rec.Code, rec.Body.String())
	}

	s.client = &wa.Client{}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v2/contacts/lookup-username", strings.NewReader(`{"username":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("client not ready status = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestWriteUsernameLookupNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/contacts/lookup-username?debug=1", nil)
	writeUsernameLookup(rec, req, wa.UsernameLookupResult{
		Username: "nobody",
		Found:    false,
		RawNode:  `<iq><usync><list/></usync></iq>`,
	})
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var got struct {
		Found    bool   `json:"found"`
		Username string `json:"username"`
		Debug    string `json:"debug"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Found || got.Username != "nobody" || got.Debug == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestHandleFetchUsernamesEmpty(t *testing.T) {
	s, _ := newUsernameTestServer(t)
	s.client = &wa.Client{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/contacts/fetch-usernames", strings.NewReader(`{"jids":[]}`))
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body.String())
	}
}

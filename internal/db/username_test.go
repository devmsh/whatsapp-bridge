package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"whatsapp-bridge-v2/internal/db"
)

func TestUsernameMigrationIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(`
		CREATE TABLE contacts (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE group_participants (
			group_jid TEXT NOT NULL,
			jid TEXT NOT NULL,
			PRIMARY KEY (group_jid, jid)
		);
	`); err != nil {
		t.Fatalf("create old tables: %v", err)
	}

	if err := db.MigrateUsernameColumns(sqlDB); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := db.MigrateUsernameColumns(sqlDB); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	for _, table := range []string{"contacts", "group_participants"} {
		if !pragmaHasColumn(t, sqlDB, table, "username") {
			t.Errorf("%s.username missing after migrate", table)
		}
	}
}

func TestNewStoreUsernameMigrationTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := db.NewStore(path)
	if err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	st.Close()
	st, err = db.NewStore(path)
	if err != nil {
		t.Fatalf("second NewStore: %v", err)
	}
	defer st.Close()
	if !pragmaHasColumn(t, st.DB, "contacts", "username") {
		t.Error("contacts.username missing")
	}
	if !pragmaHasColumn(t, st.DB, "group_participants", "username") {
		t.Error("group_participants.username missing")
	}
}

func TestStoreContactUsername(t *testing.T) {
	st := newTestStore(t)
	jid := "966500000101@s.whatsapp.net"

	if err := st.StoreContact(&db.Contact{JID: jid, Name: "Alice", Username: "@Alice"}); err != nil {
		t.Fatalf("store with username: %v", err)
	}
	got, err := st.GetContact(jid)
	if err != nil || got == nil {
		t.Fatalf("get: %v %#v", err, got)
	}
	if got.Username != "Alice" {
		t.Fatalf("username = %q, want Alice (leading @ stripped)", got.Username)
	}

	if err := st.StoreContact(&db.Contact{JID: jid, Name: "Alice", PushName: "Al"}); err != nil {
		t.Fatalf("store without username: %v", err)
	}
	got, err = st.GetContact(jid)
	if err != nil || got == nil {
		t.Fatalf("get after empty upsert: %v %#v", err, got)
	}
	if got.Username != "Alice" {
		t.Fatalf("username overwritten with empty: got %q", got.Username)
	}
	if got.PushName != "Al" {
		t.Fatalf("push_name = %q, want Al", got.PushName)
	}

	if err := st.StoreContact(&db.Contact{JID: jid, Username: "alice2"}); err != nil {
		t.Fatalf("store new username: %v", err)
	}
	got, err = st.GetContact(jid)
	if err != nil || got == nil {
		t.Fatalf("get after rename: %v %#v", err, got)
	}
	if got.Username != "alice2" {
		t.Fatalf("username = %q, want alice2", got.Username)
	}

	byUser, err := st.GetContactByUsername("ALICE2")
	if err != nil || byUser == nil {
		t.Fatalf("GetContactByUsername: %v %#v", err, byUser)
	}
	if byUser.JID != jid {
		t.Fatalf("by-username jid = %q", byUser.JID)
	}

	list, err := st.GetContacts("", "@alice2")
	if err != nil {
		t.Fatalf("GetContacts username filter: %v", err)
	}
	if len(list) != 1 || list[0].JID != jid {
		t.Fatalf("GetContacts(?username=) = %#v", list)
	}
}

func TestStoreGroupParticipantUsernamePreserved(t *testing.T) {
	st := newTestStore(t)
	g := "120363000000000002@g.us"
	jid := "966500000101@s.whatsapp.net"

	if err := st.StoreGroupParticipant(&db.GroupParticipant{
		GroupJID: g, JID: jid, DisplayName: "Alice", Username: "alice",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := st.StoreGroupParticipant(&db.GroupParticipant{
		GroupJID: g, JID: jid, DisplayName: "Alice", IsAdmin: true,
	}); err != nil {
		t.Fatalf("store without username: %v", err)
	}
	parts, err := st.GetGroupParticipants(g)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d participants", len(parts))
	}
	if parts[0].Username != "alice" {
		t.Fatalf("username = %q, want alice", parts[0].Username)
	}
	if !parts[0].IsAdmin {
		t.Fatal("expected is_admin after second upsert")
	}

	if err := st.StoreContact(&db.Contact{JID: jid, Phone: "966500000101", Username: "alice"}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	other := "120363000000000003@g.us"
	if err := st.StoreGroupParticipant(&db.GroupParticipant{
		GroupJID: other, JID: jid, Phone: "966500000101",
	}); err != nil {
		t.Fatalf("store other group: %v", err)
	}
	if err := st.StoreContact(&db.Contact{JID: jid, Phone: "966500000101", Username: "alice"}); err != nil {
		t.Fatalf("propagate: %v", err)
	}
	parts, err = st.GetGroupParticipants(other)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	if len(parts) != 1 || parts[0].Username != "alice" {
		t.Fatalf("expected username propagated onto participant, got %#v", parts)
	}
}

func pragmaHasColumn(t *testing.T, sqlDB *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := sqlDB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

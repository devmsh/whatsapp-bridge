package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// findZipFile returns the contents of the first zip entry whose name ends with
// the given suffix, or fails the test.
func findZipFile(t *testing.T, files map[string]string, suffix string) string {
	t.Helper()
	for name, body := range files {
		if strings.HasSuffix(name, suffix) {
			return body
		}
	}
	t.Fatalf("zip entry ending with %q not found; entries: %v", suffix, keysOf(files))
	return ""
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustStoreMU(t *testing.T, st *db.Store, chatJID, msgID, kind, content string) {
	t.Helper()
	if _, err := st.DB.Exec(
		`INSERT INTO media_understanding (chat_jid, message_id, kind, content, status, generated_at)
		 VALUES (?,?,?,?,?,?)`, chatJID, msgID, kind, content, db.MUOK, 1); err != nil {
		t.Fatalf("insert media_understanding: %v", err)
	}
}

func TestHandleCircleExport(t *testing.T) {
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer st.Close()

	const groupJID = "120363000000000000@g.us"
	const contactJID = "972500000001@s.whatsapp.net"

	// Circle with a group and a contact.
	circle, err := st.CreateCircle("Family", "#fff", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	if err := st.StoreChat(&db.Chat{JID: groupJID, Name: "Family Group", ChatType: "group"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.StoreContact(&db.Contact{JID: contactJID, Name: "Ahmed", Phone: "972500000001"}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	if err := st.AddCircleMember(circle.ID, db.MemberGroup, groupJID); err != nil {
		t.Fatalf("AddCircleMember group: %v", err)
	}
	if err := st.AddCircleMember(circle.ID, db.MemberContact, contactJID); err != nil {
		t.Fatalf("AddCircleMember contact: %v", err)
	}

	// Group messages covering each rendering branch.
	msgs := []*db.Message{
		{ID: "m1", ChatJID: groupJID, IsGroup: true, Sender: contactJID, SenderName: "Ahmed",
			Content: "Are we still on for Friday?", Timestamp: 1000, MessageType: "text"},
		{ID: "m2", ChatJID: groupJID, IsGroup: true, IsFromMe: true,
			Content: "Yes, 7pm", Timestamp: 1001, MessageType: "text"},
		{ID: "m3", ChatJID: groupJID, IsGroup: true, Sender: contactJID, SenderName: "Mona",
			Timestamp: 1002, MessageType: "image", MediaType: "image"}, // has description below
		{ID: "m4", ChatJID: groupJID, IsGroup: true, Sender: contactJID, SenderName: "Mona",
			Timestamp: 1003, MessageType: "image", MediaType: "image"}, // no description
		{ID: "m5", ChatJID: groupJID, IsGroup: true, Sender: contactJID, SenderName: "Ahmed",
			Timestamp: 1004, MessageType: "audio", MediaType: "audio"}, // has transcript below
		{ID: "m6", ChatJID: groupJID, IsGroup: true, IsFromMe: true,
			Timestamp: 1005, MessageType: "document", MediaType: "document", MediaFilename: "invoice.pdf"},
		{ID: "m7", ChatJID: groupJID, IsGroup: true, Sender: contactJID, SenderName: "Ahmed",
			Content: "to be removed", Timestamp: 1006, MessageType: "text", IsDeleted: true},
		{ID: "m8", ChatJID: groupJID, IsGroup: true, IsFromMe: true,
			Content: "fixed text", Timestamp: 1007, MessageType: "text", IsEdit: true},
		// A separate DM chat.
		{ID: "c1", ChatJID: contactJID, Sender: contactJID, SenderName: "Ahmed",
			Content: "hello there", Timestamp: 2000, MessageType: "text"},
	}
	for _, m := range msgs {
		if err := st.StoreMessage(m); err != nil {
			t.Fatalf("StoreMessage %s: %v", m.ID, err)
		}
	}
	mustStoreMU(t, st, groupJID, "m3", db.MUDescription, "a chocolate cake")
	mustStoreMU(t, st, groupJID, "m5", db.MUTranscript, "I'll be 10 minutes late")

	// Run the handler.
	s := &Server{store: st}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/circles/1/export", nil)
	s.handleCircleExport(rec, req, circle.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}

	// Read the zip back.
	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}

	group := findZipFile(t, files, "/Family Group.txt")
	for _, want := range []string{
		"Ahmed: Are we still on for Friday?",
		"You: Yes, 7pm",
		"<image: a chocolate cake>",
		"<image omitted>",
		`transcript: "I'll be 10 minutes late"`,
		"<document: invoice.pdf>",
		"<message deleted>",
		"fixed text (edited)",
	} {
		if !strings.Contains(group, want) {
			t.Errorf("group transcript missing %q\n--- got ---\n%s", want, group)
		}
	}

	dm := findZipFile(t, files, "/Ahmed.txt")
	if !strings.Contains(dm, "Ahmed: hello there") {
		t.Errorf("DM transcript missing line; got:\n%s", dm)
	}

	summary := findZipFile(t, files, "/_summary.txt")
	for _, want := range []string{"Circle: Family", "Family Group — 8 messages", "Ahmed — 1 messages"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q\n--- got ---\n%s", want, summary)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"Family Group":           "Family Group",
		"a/b:c*d?":               "a_b_c_d_",
		"   ":                    "chat",
		"":                       "chat",
		".hidden.":               "hidden",
		strings.Repeat("x", 100): strings.Repeat("x", 60),
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	used := map[string]bool{}
	if got := uniqueName(used, "Ahmed"); got != "Ahmed" {
		t.Errorf("first = %q, want Ahmed", got)
	}
	if got := uniqueName(used, "Ahmed"); got != "Ahmed (2)" {
		t.Errorf("second = %q, want 'Ahmed (2)'", got)
	}
	if got := uniqueName(used, "Ahmed"); got != "Ahmed (3)" {
		t.Errorf("third = %q, want 'Ahmed (3)'", got)
	}
}

package extract_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract"
)

func newStore(t *testing.T) *db.Store {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func riyadh(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		return time.UTC
	}
	return loc
}

// TestFetchLinesEnriches covers the three things the model cannot do for
// itself: hear a voice note, know who "@<digits>" is, and see that a message
// is answering another one.
func TestFetchLinesEnriches(t *testing.T) {
	st := newStore(t)
	chat := "team@g.us"

	if err := st.StoreContact(&db.Contact{
		JID: "966500000001@s.whatsapp.net", LID: "111111111111@lid", Name: "Sara Haddad",
	}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	if err := st.StoreContact(&db.Contact{
		JID: "966500000002@s.whatsapp.net", Name: "Sami Nasser",
	}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}

	if err := st.StoreMessage(&db.Message{
		ID: "M1", ChatJID: chat, Sender: "966500000002@s.whatsapp.net",
		Content: "نبدأ الخطة", Timestamp: 1000,
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	// A voice note whose words only exist as a transcript.
	if err := st.StoreMessage(&db.Message{
		ID: "M2", ChatJID: chat, Sender: "966500000001@s.whatsapp.net",
		MediaType: "voice_note", Timestamp: 1001, ReplyToID: "M1",
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	if _, err := st.DB.Exec(`INSERT INTO media_understanding
		(chat_jid, message_id, kind, content, status, generated_at)
		VALUES (?,?,?,?,?,?)`,
		chat, "M2", "transcript", "ابعتلي الملف بكرة", "ok", 1001); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	// A mention written the way WhatsApp writes it: @<digits>.
	if err := st.StoreMessage(&db.Message{
		ID: "M3", ChatJID: chat, Sender: "966500000002@s.whatsapp.net",
		Content:  "@111111111111 تابعي معه",
		Mentions: `["111111111111@lid"]`, Timestamp: 1002,
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	lines, err := extract.FetchLines(st, chat, 0, 2000)
	if err != nil {
		t.Fatalf("FetchLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	if !strings.Contains(lines[1].Text, "[transcript] ابعتلي الملف بكرة") {
		t.Errorf("voice note should carry its transcript, got %q", lines[1].Text)
	}
	if lines[1].ReplyTo != "M1" {
		t.Errorf("reply target lost: %q", lines[1].ReplyTo)
	}
	if !strings.Contains(lines[2].Text, "@Sara Haddad") {
		t.Errorf("mention should read as a name, got %q", lines[2].Text)
	}
	// The JID stays available, in phone form, for the owner resolver.
	if len(lines[2].Mentions) != 1 || lines[2].Mentions[0] != "966500000001@s.whatsapp.net" {
		t.Errorf("mention JID should be kept in phone form, got %v", lines[2].Mentions)
	}
	if lines[0].Sender != "Sami Nasser" {
		t.Errorf("sender name = %q, want Sami Nasser", lines[0].Sender)
	}
}

// TestFetchLinesWindow checks the watermark contract: since is exclusive, so a
// watermark can be handed straight back without re-reading its last message.
func TestFetchLinesWindow(t *testing.T) {
	st := newStore(t)
	chat := "c@g.us"
	for i, ts := range []int64{100, 200, 300} {
		if err := st.StoreMessage(&db.Message{
			ID: string(rune('A' + i)), ChatJID: chat, Content: "x", Timestamp: ts,
		}); err != nil {
			t.Fatalf("StoreMessage: %v", err)
		}
	}
	lines, err := extract.FetchLines(st, chat, 200, 300)
	if err != nil {
		t.Fatalf("FetchLines: %v", err)
	}
	if len(lines) != 1 || lines[0].TS != 300 {
		t.Fatalf("window should be (since, until], got %d lines", len(lines))
	}
}

// TestRenderQuotesMessageIDs is the contract that makes evidence checkable:
// every line the model reads is addressable by an id it could not invent.
func TestRenderQuotesMessageIDs(t *testing.T) {
	c := extract.Chunk{Lines: []extract.Line{
		{MessageID: "AAA", TS: 1_760_000_000, Sender: "Sara", Text: "جهز العقد"},
		{MessageID: "BBB", TS: 1_760_000_060, Sender: "Sami", Text: "تمام", ReplyTo: "AAA"},
		{MessageID: "CCC", TS: 1_760_000_120, Sender: "Sara", Text: "old", Context: true},
	}}
	out := extract.Render(c, riyadh(t))
	for _, want := range []string{"[#AAA]", "[#BBB]", "↳ replying to #AAA", "[context] [#CCC]"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered chunk missing %q\n%s", want, out)
		}
	}
}

// TestRosterCarriesKunya checks that the user's own notes reach the model.
// A chat where people address each other as "أبو يمان" is unreadable without
// them.
func TestRosterCarriesKunya(t *testing.T) {
	st := newStore(t)
	chat := "g@g.us"
	if err := st.StoreContact(&db.Contact{
		JID: "966500000003@s.whatsapp.net", Name: "Mohammed Qudaih",
	}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	// Written through the notes path on purpose: contact sync never touches
	// these two columns, which is what keeps them safe from a refresh.
	if err := st.SetContactNotes("966500000003@s.whatsapp.net",
		"أبو يمان", "Introduced by Abdullah about the QRT file"); err != nil {
		t.Fatalf("SetContactNotes: %v", err)
	}
	if err := st.StoreGroupParticipant(&db.GroupParticipant{
		GroupJID: chat, JID: "966500000003@s.whatsapp.net", IsAdmin: true,
	}); err != nil {
		t.Fatalf("StoreGroupParticipant: %v", err)
	}

	people, own, isGroup, err := extract.Roster(st, chat)
	if err != nil {
		t.Fatalf("Roster: %v", err)
	}
	if !isGroup {
		t.Errorf("a @g.us chat is a group")
	}
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if people[0].Kunya != "أبو يمان" || people[0].HowWeMet == "" {
		t.Errorf("kunya and how-we-met should reach the roster: %+v", people[0])
	}

	block := extract.RenderRoster(people, own)
	if !strings.Contains(block, "أبو يمان") || !strings.Contains(block, "QRT") {
		t.Errorf("rendered roster should mention both:\n%s", block)
	}
	if !strings.Contains(block, "[admin]") {
		t.Errorf("admin flag should be visible:\n%s", block)
	}
}

// TestNameResolverPrefersRealNumbers guards against the junk identity rows in
// this database: 129 contacts store a LID under the phone server, so the same
// digits exist as both "<lid>@lid" and "<lid>@s.whatsapp.net". Whichever row
// loaded last used to win, and an owner could resolve to a number nothing else
// in the app knows.
func TestNameResolverPrefersRealNumbers(t *testing.T) {
	st := newStore(t)
	chat := "g@g.us"

	// The real person: a phone number, with their LID recorded.
	if err := st.StoreContact(&db.Contact{
		JID: "966500000101@s.whatsapp.net", LID: "10000000000101", Phone: "966500000101",
		Name: "Mohammed Hasan",
	}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	// The artefacts: the same LID as its own contact, twice over.
	for _, jid := range []string{"10000000000101@lid", "10000000000101@s.whatsapp.net"} {
		if err := st.StoreContact(&db.Contact{
			JID: jid, Phone: "10000000000101", Name: "Mohammed Ali Hasan",
		}); err != nil {
			t.Fatalf("StoreContact(%s): %v", jid, err)
		}
	}

	if err := st.StoreMessage(&db.Message{
		ID: "M1", ChatJID: chat, Sender: "966500000009@s.whatsapp.net",
		Content: "@10000000000101 جهز العقد", Mentions: `["10000000000101@lid"]`,
		Timestamp: 1000,
	}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	lines, err := extract.FetchLines(st, chat, 0, 2000)
	if err != nil {
		t.Fatalf("FetchLines: %v", err)
	}
	if len(lines) != 1 || len(lines[0].Mentions) != 1 {
		t.Fatalf("expected one message with one mention, got %+v", lines)
	}
	if got := lines[0].Mentions[0]; got != "966500000101@s.whatsapp.net" {
		t.Errorf("mention resolved to %q, want the real number — a LID under the\n"+
			"phone server is a sync artefact, not somebody's contact", got)
	}
}

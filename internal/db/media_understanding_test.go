package db_test

import (
	"path/filepath"
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seedTranscript inserts an 'ok' transcript row that has not been refined yet.
func seedTranscript(t *testing.T, st *db.Store, msgID, content string) {
	t.Helper()
	if _, err := st.DB.Exec(
		`INSERT INTO messages (chat_jid, id, sender, media_type, media_path, timestamp)
		 VALUES (?,?,?,?,?,?)`,
		"c@g.us", msgID, "s@s.whatsapp.net", "voice_note", "/tmp/x.ogg", 1,
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := st.UpsertMU(&db.MediaUnderstanding{
		ChatJID: "c@g.us", MessageID: msgID, Kind: db.MUTranscript,
		Content: content, Status: db.MUOK, Refined: 0,
	}); err != nil {
		t.Fatalf("seed mu: %v", err)
	}
}

func containsID(targets []db.RefineTarget, msgID string) bool {
	for _, tg := range targets {
		if tg.MessageID == msgID {
			return true
		}
	}
	return false
}

// The regression test for the runaway Codex loop.
//
// Three real voice notes ("ده", "موسيقى موسيقى", "نفس الشيء.") were too short
// for the transcript cleaner to change. The worker treated "text unchanged" as
// failure and never set refined=1, so the 15s backfill loop re-selected them
// and burned a Codex call every cycle for eight days.
//
// Marking the refine pass complete must remove the row from the queue even
// though its content was never rewritten.
func TestMarkTranscriptRefinedStopsReselection(t *testing.T) {
	st := newTestStore(t)
	seedTranscript(t, st, "M1", "ده")

	if !containsID(st.PendingTranscriptsToRefine(10), "M1") {
		t.Fatal("row should be queued before the refine pass runs")
	}

	if err := st.MarkTranscriptRefined("c@g.us", "M1"); err != nil {
		t.Fatalf("MarkTranscriptRefined: %v", err)
	}

	if containsID(st.PendingTranscriptsToRefine(10), "M1") {
		t.Fatal("row was re-selected after a completed refine pass — the loop is unbounded")
	}

	// Content must be preserved untouched: the cleaner had nothing to change.
	mu, err := st.GetMU("c@g.us", "M1", db.MUTranscript)
	if err != nil {
		t.Fatalf("GetMU: %v", err)
	}
	if mu.Content != "ده" {
		t.Fatalf("content was modified: got %q, want %q", mu.Content, "ده")
	}
	if mu.Refined != 1 {
		t.Fatalf("refined flag not set: got %d, want 1", mu.Refined)
	}
}

// A genuinely failing refiner (empty output) must retry, but not forever.
func TestRefineAttemptsBoundRetries(t *testing.T) {
	st := newTestStore(t)
	seedTranscript(t, st, "M2", "some long enough raw transcript text")

	for i := 1; i <= db.MaxRefineAttempts; i++ {
		if !containsID(st.PendingTranscriptsToRefine(10), "M2") {
			t.Fatalf("row dropped from the queue after %d attempts, before the cap of %d",
				i-1, db.MaxRefineAttempts)
		}
		if err := st.BumpRefineAttempt("c@g.us", "M2"); err != nil {
			t.Fatalf("BumpRefineAttempt: %v", err)
		}
	}

	if containsID(st.PendingTranscriptsToRefine(10), "M2") {
		t.Fatalf("row still queued after %d failed attempts — retries are unbounded",
			db.MaxRefineAttempts)
	}
}

// A successful rewrite must also leave the queue.
func TestSetTranscriptRefinedStopsReselection(t *testing.T) {
	st := newTestStore(t)
	seedTranscript(t, st, "M3", "raw unpunctuated text")

	if err := st.SetTranscriptRefined("c@g.us", "M3", "Raw, punctuated text."); err != nil {
		t.Fatalf("SetTranscriptRefined: %v", err)
	}

	if containsID(st.PendingTranscriptsToRefine(10), "M3") {
		t.Fatal("row re-selected after a successful refine")
	}
	mu, _ := st.GetMU("c@g.us", "M3", db.MUTranscript)
	if mu.Content != "Raw, punctuated text." {
		t.Fatalf("refined content not stored: got %q", mu.Content)
	}
}

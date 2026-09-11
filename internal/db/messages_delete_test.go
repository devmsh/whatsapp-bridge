package db_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// TestMarkChatMessagesDeletedIsTimeBounded reproduces the "messages show as
// deleted but the real app still has them" bug. A clear-chat from months ago
// was replayed by a full app-state sync and swallowed every message sent since,
// stamping them as deleted before they were even sent.
func TestMarkChatMessagesDeletedIsTimeBounded(t *testing.T) {
	st := newTestStore(t)

	chat := "committee@g.us"
	const clearedAt int64 = 1000

	older := &db.Message{ID: "old", ChatJID: chat, Content: "before the clear", Timestamp: 500}
	newer := &db.Message{ID: "new", ChatJID: chat, Content: "sent afterwards", Timestamp: 2000}
	for _, m := range []*db.Message{older, newer} {
		if err := st.StoreMessage(m); err != nil {
			t.Fatalf("StoreMessage(%s): %v", m.ID, err)
		}
	}

	n, err := st.MarkChatMessagesDeleted(chat, "me@s.whatsapp.net", clearedAt)
	if err != nil {
		t.Fatalf("MarkChatMessagesDeleted: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked %d messages deleted, want 1 (only the one that existed at the time)", n)
	}

	msgs, err := st.GetMessages(chat, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range msgs {
		seen[m.ID] = m.IsDeleted
	}
	if !seen["old"] {
		t.Errorf("message sent before the clear should be marked deleted")
	}
	if seen["new"] {
		t.Errorf("message sent after the clear must stay visible")
	}

	// Replaying the very same clear must not swallow the later message either.
	if _, err := st.MarkChatMessagesDeleted(chat, "me@s.whatsapp.net", clearedAt); err != nil {
		t.Fatalf("replay: %v", err)
	}
	msgs, _ = st.GetMessages(chat, 0, 10)
	for _, m := range msgs {
		if m.ID == "new" && m.IsDeleted {
			t.Errorf("replaying an old clear-chat deleted a newer message")
		}
	}
}

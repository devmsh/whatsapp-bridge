package db_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// TestMarkingReadKeepsChatFlags reproduces the bug where pins kept vanishing.
// "Mark as read" knows only a JID and an unread count; when that went through
// StoreChat's INSERT OR REPLACE, every other column was reset to its zero
// value — so reading a chat unpinned it, unarchived it, unmuted it and wiped
// its last-message time.
func TestMarkingReadKeepsChatFlags(t *testing.T) {
	st := newTestStore(t)

	jid := "one-studio@g.us"
	if err := st.StoreChat(&db.Chat{
		JID:           jid,
		Name:          "North Studio",
		LastMessageAt: 1700,
		UnreadCount:   4,
		IsPinned:      true,
		IsArchived:    true,
		IsMuted:       true,
		MutedUntil:    9999,
	}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}

	if err := st.SetChatUnread(jid, 0); err != nil {
		t.Fatalf("SetChatUnread: %v", err)
	}

	got, err := st.GetChat(jid)
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if got.UnreadCount != 0 {
		t.Errorf("unread = %d, want 0", got.UnreadCount)
	}
	if !got.IsPinned {
		t.Errorf("marking a chat read unpinned it")
	}
	if !got.IsArchived {
		t.Errorf("marking a chat read unarchived it")
	}
	if !got.IsMuted || got.MutedUntil != 9999 {
		t.Errorf("marking a chat read cleared the mute")
	}
	if got.LastMessageAt != 1700 {
		t.Errorf("last_message_at = %d, want 1700 — marking read wiped the timestamp", got.LastMessageAt)
	}
	if got.Name != "North Studio" {
		t.Errorf("name = %q, want %q", got.Name, "North Studio")
	}
}

// TestStoreChatKeepsDeletedFlag guards the other half: deleted_at is the
// bridge's own state, so a sync writing a chat record must not resurrect a
// chat the user deleted in WhatsApp.
func TestStoreChatKeepsDeletedFlag(t *testing.T) {
	st := newTestStore(t)

	jid := "gone@g.us"
	if err := st.StoreChat(&db.Chat{JID: jid, Name: "Gone"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.MarkChatDeleted(jid, 1000); err != nil {
		t.Fatalf("MarkChatDeleted: %v", err)
	}

	// History sync re-writes the record with fresh metadata.
	if err := st.StoreChat(&db.Chat{JID: jid, Name: "Gone", LastMessageAt: 500}); err != nil {
		t.Fatalf("StoreChat (resync): %v", err)
	}
	if !st.IsChatDeleted(jid) {
		t.Fatalf("a chat sync resurrected a deleted chat")
	}
}

package db_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// TestGetChatsHidesEmptyAndDeleted covers the two rules the chat list applies:
// a chat that never had a message is noise from contact/group sync, and a chat
// deleted in WhatsApp is gone there, so it must be gone here.
func TestGetChatsHidesEmptyAndDeleted(t *testing.T) {
	st := newTestStore(t)

	for _, jid := range []string{"real@g.us", "empty@g.us", "deleted@g.us"} {
		if err := st.StoreChat(&db.Chat{JID: jid, Name: jid}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
	}
	for _, jid := range []string{"real@g.us", "deleted@g.us"} {
		if err := st.StoreMessage(&db.Message{
			ID: "m-" + jid, ChatJID: jid, Content: "hello", Timestamp: 500,
		}); err != nil {
			t.Fatalf("StoreMessage(%s): %v", jid, err)
		}
	}
	if err := st.MarkChatDeleted("deleted@g.us", 900); err != nil {
		t.Fatalf("MarkChatDeleted: %v", err)
	}

	chats, err := st.GetChats()
	if err != nil {
		t.Fatalf("GetChats: %v", err)
	}
	if len(chats) != 1 || chats[0].JID != "real@g.us" {
		got := make([]string, len(chats))
		for i, c := range chats {
			got[i] = c.JID
		}
		t.Fatalf("GetChats = %v, want only [real@g.us]", got)
	}
	if !st.IsChatDeleted("deleted@g.us") {
		t.Errorf("IsChatDeleted = false, want true")
	}
}

// TestDeletedChatReturnsOnNewMessage matches WhatsApp: delete a conversation
// and it disappears, but a message that arrives afterwards brings it back.
// Older backfill must not resurrect it.
func TestDeletedChatReturnsOnNewMessage(t *testing.T) {
	st := newTestStore(t)

	jid := "gone@s.whatsapp.net"
	if err := st.StoreChat(&db.Chat{JID: jid, Name: "Gone"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.StoreMessage(&db.Message{ID: "old", ChatJID: jid, Timestamp: 500}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	if err := st.MarkChatDeleted(jid, 1000); err != nil {
		t.Fatalf("MarkChatDeleted: %v", err)
	}

	// Backfill of an older message must leave the chat deleted.
	if err := st.UpdateChatLastMessage(jid, "Gone", 600); err != nil {
		t.Fatalf("UpdateChatLastMessage(old): %v", err)
	}
	if !st.IsChatDeleted(jid) {
		t.Fatalf("an older message resurrected a deleted chat")
	}

	// A genuinely newer message brings it back.
	if err := st.UpdateChatLastMessage(jid, "Gone", 2000); err != nil {
		t.Fatalf("UpdateChatLastMessage(new): %v", err)
	}
	if st.IsChatDeleted(jid) {
		t.Fatalf("a newer message should bring the chat back")
	}
	chats, err := st.GetChats()
	if err != nil {
		t.Fatalf("GetChats: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("expected the chat back in the list, got %d rows", len(chats))
	}
}

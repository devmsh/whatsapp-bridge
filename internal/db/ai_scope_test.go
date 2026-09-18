package db_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// TestAIExcludedJIDs checks the rule that archived chats are as invisible to
// the AI as hidden ones: a group you left, or a client you archived, must not
// come back through any AI path.
func TestAIExcludedJIDs(t *testing.T) {
	st := newTestStore(t)

	plain := "work@g.us"
	hidden := "private@s.whatsapp.net"
	archived := "finished@g.us"

	for _, jid := range []string{plain, hidden, archived} {
		if err := st.StoreChat(&db.Chat{JID: jid}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
	}
	if err := st.HideChat(hidden); err != nil {
		t.Fatalf("HideChat: %v", err)
	}
	if err := st.SetChatArchived(archived, true); err != nil {
		t.Fatalf("SetChatArchived: %v", err)
	}

	excluded := st.AIExcludedJIDs()
	if excluded[plain] {
		t.Errorf("a plain chat must stay available to the AI")
	}
	if !excluded[hidden] {
		t.Errorf("hidden chat missing from the AI-excluded set")
	}
	if !excluded[archived] {
		t.Errorf("archived chat missing from the AI-excluded set")
	}

	if st.IsChatExcludedFromAI(plain) {
		t.Errorf("IsChatExcludedFromAI(plain) = true, want false")
	}
	if !st.IsChatExcludedFromAI(hidden) {
		t.Errorf("IsChatExcludedFromAI(hidden) = false, want true")
	}
	if !st.IsChatExcludedFromAI(archived) {
		t.Errorf("IsChatExcludedFromAI(archived) = false, want true")
	}

	// Unarchiving brings the chat straight back — membership rows were never
	// touched, so nothing has to be re-added by hand.
	if err := st.SetChatArchived(archived, false); err != nil {
		t.Fatalf("SetChatArchived(false): %v", err)
	}
	if st.IsChatExcludedFromAI(archived) {
		t.Errorf("unarchived chat should be available to the AI again")
	}
}

// TestFlattenCircleChatsSkipsExcluded covers the choke point every circle-level
// AI job uses. Archived and hidden members must not be handed to extraction,
// digests or briefings, while the rest of the circle still comes through.
func TestFlattenCircleChatsSkipsExcluded(t *testing.T) {
	st := newTestStore(t)

	circle, err := st.CreateCircle("NorthStudio", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}

	live := "live@g.us"
	left := "left@g.us"
	private := "private@s.whatsapp.net"

	for _, jid := range []string{live, left, private} {
		if err := st.StoreChat(&db.Chat{JID: jid}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
		memberType := db.MemberGroup
		if jid == private {
			memberType = db.MemberContact
		}
		if err := st.AddCircleMember(circle.ID, memberType, jid); err != nil {
			t.Fatalf("AddCircleMember(%s): %v", jid, err)
		}
	}

	if err := st.SetChatArchived(left, true); err != nil {
		t.Fatalf("SetChatArchived: %v", err)
	}
	if err := st.HideChat(private); err != nil {
		t.Fatalf("HideChat: %v", err)
	}

	jids, err := st.FlattenCircleChats(circle.ID)
	if err != nil {
		t.Fatalf("FlattenCircleChats: %v", err)
	}
	if len(jids) != 1 || jids[0] != live {
		t.Fatalf("FlattenCircleChats = %v, want only [%s]", jids, live)
	}
}

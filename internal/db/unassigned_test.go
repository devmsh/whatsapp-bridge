package db_test

import (
	"fmt"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// TestUnassignedGroups checks the virtual "Unassigned" circle: it must hold
// only groups that are live and still need sorting. A group in a circle, one
// you left, an archived one, a hidden one and one with no messages at all stay
// out, so the list is always a to-do list and never a graveyard.
func TestUnassignedGroups(t *testing.T) {
	st := newTestStore(t)

	circle, err := st.CreateCircle("OneStudio", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}

	groups := map[string]string{
		"todo":     "Needs A Circle",
		"incircle": "Already Sorted",
		"left":     "Old Project",
		"archived": "Finished Client",
		"hidden":   "Family Group",
		"silent":   "Nobody Ever Wrote Here",
	}
	for key, name := range groups {
		jid := key + "@g.us"
		if err := st.StoreGroup(&db.Group{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreGroup(%s): %v", jid, err)
		}
		if err := st.StoreChat(&db.Chat{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
		// "silent" stays empty: a group nobody ever wrote in is sync noise,
		// not something to file, so it must never show up.
		if key == "silent" {
			continue
		}
		if err := st.StoreMessage(&db.Message{
			ID: "m-" + key, ChatJID: jid, Content: "hello", Timestamp: 500,
		}); err != nil {
			t.Fatalf("StoreMessage(%s): %v", jid, err)
		}
	}

	if err := st.AddCircleMember(circle.ID, db.MemberGroup, "incircle@g.us"); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}
	if err := st.MarkGroupLeft("left@g.us", "", 1000); err != nil {
		t.Fatalf("MarkGroupLeft: %v", err)
	}
	if err := st.SetChatArchived("archived@g.us", true); err != nil {
		t.Fatalf("SetChatArchived: %v", err)
	}
	if err := st.HideChat("hidden@g.us"); err != nil {
		t.Fatalf("HideChat: %v", err)
	}

	got, err := st.UnassignedGroups()
	if err != nil {
		t.Fatalf("UnassignedGroups: %v", err)
	}
	if len(got) != 1 || got[0].JID != "todo@g.us" {
		names := make([]string, len(got))
		for i, g := range got {
			names[i] = g.JID
		}
		t.Fatalf("UnassignedGroups = %v, want only [todo@g.us]", names)
	}
	if got[0].Name != "Needs A Circle" {
		t.Errorf("name = %q, want %q", got[0].Name, "Needs A Circle")
	}

	// Once sorted into a circle it must drop out of the list.
	if err := st.AddCircleMember(circle.ID, db.MemberGroup, "todo@g.us"); err != nil {
		t.Fatalf("AddCircleMember(todo): %v", err)
	}
	got, err = st.UnassignedGroups()
	if err != nil {
		t.Fatalf("UnassignedGroups after assign: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty list after assigning, got %d", len(got))
	}
}

// TestUnassignedPeople covers the people half of the list: a direct chat you
// actually use, not yet in any circle. The identity check must see through the
// LID/phone split — someone filed under their phone JID while the chat sits
// under their @lid is already sorted, and must not be offered again.
func TestUnassignedPeople(t *testing.T) {
	st := newTestStore(t)

	circle, err := st.CreateCircle("OneStudio", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}

	// A person worth filing.
	loose := "20100@s.whatsapp.net"
	// The same person under two identities: chat stored by LID, filed by phone.
	lidChat := "55500@lid"
	phoneRef := "20200@s.whatsapp.net"
	// Your own notes inbox.
	self := "96650@s.whatsapp.net"

	for _, jid := range []string{loose, lidChat, self} {
		if err := st.StoreChat(&db.Chat{JID: jid, Name: jid}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
		if err := st.StoreMessage(&db.Message{
			ID: "m-" + jid, ChatJID: jid, Content: "hi", Timestamp: 500,
		}); err != nil {
			t.Fatalf("StoreMessage(%s): %v", jid, err)
		}
	}
	if err := st.StoreContact(&db.Contact{JID: phoneRef, LID: lidChat, Name: "Filed Already"}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	if err := st.AddCircleMember(circle.ID, db.MemberContact, phoneRef); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}

	got, err := st.UnassignedPeople("96650")
	if err != nil {
		t.Fatalf("UnassignedPeople: %v", err)
	}
	if len(got) != 1 || got[0].JID != loose {
		names := make([]string, len(got))
		for i, g := range got {
			names[i] = g.JID
		}
		t.Fatalf("UnassignedPeople = %v, want only [%s]", names, loose)
	}
	if got[0].Kind != "contact" {
		t.Errorf("kind = %q, want %q", got[0].Kind, "contact")
	}
}

// TestUnassignedRankedByRecentActivity checks the ordering rule: the list is a
// "what needs filing now" queue, so a chat busy this month beats a bigger but
// dormant one.
func TestUnassignedRankedByRecentActivity(t *testing.T) {
	st := newTestStore(t)

	now := time.Now().Unix()
	recent := now - 2*86400 // inside the 30-day window
	old := now - 200*86400  // well outside it

	mk := func(jid, name string, recentMsgs, oldMsgs int) {
		if err := st.StoreGroup(&db.Group{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreGroup(%s): %v", jid, err)
		}
		if err := st.StoreChat(&db.Chat{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
		for i := 0; i < recentMsgs; i++ {
			st.StoreMessage(&db.Message{ID: fmt.Sprintf("%s-r%d", jid, i), ChatJID: jid, Timestamp: recent})
		}
		for i := 0; i < oldMsgs; i++ {
			st.StoreMessage(&db.Message{ID: fmt.Sprintf("%s-o%d", jid, i), ChatJID: jid, Timestamp: old})
		}
	}

	// Huge but dormant.
	mk("dormant@g.us", "Old Big Project", 0, 40)
	// Small but live right now.
	mk("live@g.us", "This Week", 5, 0)

	got, err := st.UnassignedGroups()
	if err != nil {
		t.Fatalf("UnassignedGroups: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both groups, got %d", len(got))
	}
	if got[0].JID != "live@g.us" {
		t.Fatalf("order = [%s, %s]; the chat active this month must come first",
			got[0].JID, got[1].JID)
	}
	if got[0].RecentCount != 5 || got[1].RecentCount != 0 {
		t.Errorf("recent counts = %d and %d, want 5 and 0", got[0].RecentCount, got[1].RecentCount)
	}
}

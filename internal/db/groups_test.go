package db_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/db"
)

// TestMarkGroupLeft reproduces the "exited group on another device" bug:
// once a group is no longer in the authoritative joined-groups list, the
// bridge must mark it left and drop the local account's own participant
// row, while leaving the rest of the roster intact as history.
func TestMarkGroupLeft(t *testing.T) {
	st := newTestStore(t)

	groupJID := "123@g.us"
	ownJID := "111@s.whatsapp.net"
	otherJID := "222@s.whatsapp.net"

	if err := st.StoreGroup(&db.Group{JID: groupJID, Name: "DS Support"}); err != nil {
		t.Fatalf("StoreGroup: %v", err)
	}
	if err := st.StoreGroupParticipant(&db.GroupParticipant{GroupJID: groupJID, JID: ownJID}); err != nil {
		t.Fatalf("StoreGroupParticipant(own): %v", err)
	}
	if err := st.StoreGroupParticipant(&db.GroupParticipant{GroupJID: groupJID, JID: otherJID}); err != nil {
		t.Fatalf("StoreGroupParticipant(other): %v", err)
	}

	active, err := st.GetActiveGroupJIDs()
	if err != nil {
		t.Fatalf("GetActiveGroupJIDs: %v", err)
	}
	if len(active) != 1 || active[0] != groupJID {
		t.Fatalf("expected group to be active before leaving, got %v", active)
	}

	if err := st.MarkGroupLeft(groupJID, ownJID, 1000); err != nil {
		t.Fatalf("MarkGroupLeft: %v", err)
	}

	g, err := st.GetGroup(groupJID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.LeftAt != 1000 {
		t.Fatalf("expected left_at=1000, got %d", g.LeftAt)
	}

	active, err = st.GetActiveGroupJIDs()
	if err != nil {
		t.Fatalf("GetActiveGroupJIDs: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active groups after leaving, got %v", active)
	}

	parts, err := st.GetGroupParticipants(groupJID)
	if err != nil {
		t.Fatalf("GetGroupParticipants: %v", err)
	}
	if len(parts) != 1 || parts[0].JID != otherJID {
		t.Fatalf("expected only the other participant to remain, got %v", parts)
	}
}

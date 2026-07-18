package api

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// newTestAutoExtractor builds an AutoExtractor backed by a throwaway store.
func newTestAutoExtractor(t *testing.T) (*AutoExtractor, *db.Store) {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return newAutoExtractor(&Server{store: st}), st
}

// markExtracted writes a watermark row the way handleExtractionMark does:
// last_msg_ts is a *message* timestamp, updated_at is the *run* time.
func markExtracted(t *testing.T, st *db.Store, jid string, lastMsgTS, updatedAt int64) {
	t.Helper()
	if _, err := st.DB.Exec(`INSERT INTO chat_extraction_state
		(chat_jid, last_msg_ts, last_session_id, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			last_msg_ts = excluded.last_msg_ts,
			updated_at  = excluded.updated_at`,
		jid, lastMsgTS, "sess", updatedAt); err != nil {
		t.Fatalf("mark %s: %v", jid, err)
	}
}

// TestPickDueCircleRespectsInterval reproduces the shape of the real circle 22
// that caused 1005 back-to-back auto runs: an active group plus a dormant chat
// whose newest message is ~9 months old, plus one chat that never gets a
// watermark row at all. The dormant chat used to pin the circle to
// "permanently overdue" because the interval was compared against
// min(last_msg_ts) — a message timestamp — instead of the last run time.
func TestPickDueCircleRespectsInterval(t *testing.T) {
	a, st := newTestAutoExtractor(t)
	st.PutSyncState(autoExtractIntervalKey, "8") // 8 hour interval

	now := time.Now().Unix()
	const (
		activeJID   = "120363000000000001@g.us"
		dormantJID  = "972500000002@s.whatsapp.net"
		unmarkedJID = "120363000000000002@g.us"
	)

	circle, err := st.CreateCircle("iStoria", "#fff", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	for _, jid := range []string{activeJID, dormantJID, unmarkedJID} {
		typ := db.MemberGroup
		if jid == dormantJID {
			typ = db.MemberContact
		}
		if err := st.StoreChat(&db.Chat{JID: jid, Name: jid, ChatType: "group"}); err != nil {
			t.Fatalf("StoreChat: %v", err)
		}
		if err := st.AddCircleMember(circle.ID, typ, jid); err != nil {
			t.Fatalf("AddCircleMember: %v", err)
		}
	}

	// Active group: a message 1 hour ago, newer than its watermark => hasNew.
	if err := st.StoreMessage(&db.Message{ID: "a1", ChatJID: activeJID, IsGroup: true,
		Content: "new work", Timestamp: now - 3600, MessageType: "text"}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	// Dormant chat: newest message ~9 months old. This is the value the old
	// code used as the "gap", making the circle look 291 days overdue.
	nineMonthsAgo := now - 270*86400
	if err := st.StoreMessage(&db.Message{ID: "d1", ChatJID: dormantJID,
		Content: "old", Timestamp: nineMonthsAgo, MessageType: "text"}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	// Unmarked chat: has messages but will never get a watermark row, so
	// hasNew stays true for this circle forever.
	if err := st.StoreMessage(&db.Message{ID: "u1", ChatJID: unmarkedJID, IsGroup: true,
		Content: "orphan", Timestamp: now - 7200, MessageType: "text"}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	// The circle was extracted 30 minutes ago. Watermarks record the message
	// timestamps they saw (old), but updated_at records the real run time.
	ranAt := now - 1800
	markExtracted(t, st, activeJID, now-7200, ranAt)
	markExtracted(t, st, dormantJID, nineMonthsAgo, ranAt)

	// 30 minutes < 8 hours => must NOT be due.
	if _, _, ok := a.pickDueCircle(); ok {
		t.Fatal("circle picked 30 minutes after a run with an 8h interval — interval not respected")
	}

	// Now pretend the last run was 9 hours ago. It should become due.
	nineHoursAgo := now - 9*3600
	markExtracted(t, st, activeJID, now-7200, nineHoursAgo)
	markExtracted(t, st, dormantJID, nineMonthsAgo, nineHoursAgo)
	id, name, ok := a.pickDueCircle()
	if !ok {
		t.Fatal("circle not picked 9 hours after a run with an 8h interval")
	}
	if id != circle.ID || name != "iStoria" {
		t.Fatalf("picked wrong circle: id=%d name=%q", id, name)
	}
}

// TestPickDueCircleCooldownStamp checks the safety net: even with no watermark
// rows at all (the state that made hasNew permanently true), the per-circle
// cooldown stamp alone blocks a re-run inside the interval.
func TestPickDueCircleCooldownStamp(t *testing.T) {
	a, st := newTestAutoExtractor(t)
	st.PutSyncState(autoExtractIntervalKey, "8")

	now := time.Now().Unix()
	const jid = "120363000000000003@g.us"

	circle, err := st.CreateCircle("NoWatermarks", "#fff", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	if err := st.StoreChat(&db.Chat{JID: jid, Name: jid, ChatType: "group"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.AddCircleMember(circle.ID, db.MemberGroup, jid); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}
	if err := st.StoreMessage(&db.Message{ID: "m1", ChatJID: jid, IsGroup: true,
		Content: "hi", Timestamp: now - 600, MessageType: "text"}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	// Never extracted: due immediately.
	if _, _, ok := a.pickDueCircle(); !ok {
		t.Fatal("never-extracted circle with new messages should be due")
	}

	// Stamp the cooldown as tick() does, then it must go quiet — even though
	// there are still no watermark rows and hasNew is still true.
	a.markAutoRun(circle.ID)
	if _, _, ok := a.pickDueCircle(); ok {
		t.Fatal("cooldown stamp did not block an immediate re-run")
	}

	// A stamp for a different circle must not clear this circle's cooldown.
	st.PutSyncState(autoExtractLastPrefix+"9999", "0")
	if _, _, ok := a.pickDueCircle(); ok {
		t.Fatal("another circle's cooldown stamp cleared this one")
	}

	// Backdate this circle's own stamp past the interval; it becomes due again.
	st.PutSyncState(autoExtractLastPrefix+strconv.FormatInt(circle.ID, 10),
		strconv.FormatInt(now-9*3600, 10))
	if _, _, ok := a.pickDueCircle(); !ok {
		t.Fatal("circle still blocked 9 hours after its cooldown stamp")
	}
}

// TestPickDueCircleSkipsWhenNothingNew verifies a fully caught-up circle is
// never picked, regardless of how old its messages are.
func TestPickDueCircleSkipsWhenNothingNew(t *testing.T) {
	a, st := newTestAutoExtractor(t)
	now := time.Now().Unix()
	const jid = "120363000000000004@g.us"

	circle, err := st.CreateCircle("Quiet", "#fff", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	if err := st.StoreChat(&db.Chat{JID: jid, Name: jid, ChatType: "group"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.AddCircleMember(circle.ID, db.MemberGroup, jid); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}
	msgTS := now - 400*86400
	if err := st.StoreMessage(&db.Message{ID: "m1", ChatJID: jid, IsGroup: true,
		Content: "ancient", Timestamp: msgTS, MessageType: "text"}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	// Watermark is caught up to the newest message, run was long ago.
	markExtracted(t, st, jid, msgTS, now-400*86400)

	if _, _, ok := a.pickDueCircle(); ok {
		t.Fatal("caught-up circle should never be picked")
	}
}

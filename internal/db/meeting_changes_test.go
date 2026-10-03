package db_test

import (
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// seedChatMessage stores one real message in a chat, the way the tests in
// messages_delete_test.go do it, so MeetingFollowUpsForChat has something
// real to count.
func seedChatMessage(t *testing.T, st *db.Store, chatJID, id string, ts int64) {
	t.Helper()
	if err := st.StoreMessage(&db.Message{ID: id, ChatJID: chatJID, Content: "x", Timestamp: ts}); err != nil {
		t.Fatalf("StoreMessage(%s): %v", id, err)
	}
}

// TestApplyMeetingPatchWritesHistory covers the normal case: two fields
// change, each gets its own change row with the right old/new text, and the
// evidence message is linked as an "update".
func TestApplyMeetingPatchWritesHistory(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{Title: "Weekly sync", Status: db.MeetingProposed})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	newStatus := db.MeetingConfirmed
	newStarts := int64(5_000_000_000)
	changes, err := st.ApplyMeetingPatch(m.ID, db.MeetingPatch{
		Status:    &newStatus,
		StartsAt:  &newStarts,
		Note:      "confirmed tonight",
		Source:    db.ChangeByModel,
		ChatJID:   "amer@s.whatsapp.net",
		MessageID: "m1",
	})
	if err != nil {
		t.Fatalf("ApplyMeetingPatch: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2", len(changes))
	}
	byField := map[string]db.MeetingChange{}
	for _, c := range changes {
		byField[c.Field] = c
	}
	if c := byField["status"]; c.OldValue != "proposed" || c.NewValue != "confirmed" {
		t.Errorf("status change = %+v, want proposed -> confirmed", c)
	}
	if c := byField["starts_at"]; c.OldValue != "0" || c.NewValue != "5000000000" {
		t.Errorf("starts_at change = %+v, want 0 -> 5000000000", c)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingConfirmed || got.StartsAt != newStarts {
		t.Errorf("meeting row did not update: %+v", got)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("GetMeeting should load Changes, got %d", len(got.Changes))
	}

	msgs, err := st.MeetingMessages(m.ID)
	if err != nil {
		t.Fatalf("MeetingMessages: %v", err)
	}
	found := false
	for _, mm := range msgs {
		if mm.MessageID == "m1" && mm.Role == "update" {
			found = true
		}
	}
	if !found {
		t.Errorf("evidence message m1 should be linked with role 'update', got %+v", msgs)
	}
}

// TestApplyMeetingPatchNoopWritesNothing checks that patching a field to its
// current value writes no history and does not touch updated_at.
//
// updated_at is set by hand to an old, fixed value first. Comparing against
// whatever CreateMeeting happened to set would not actually test anything:
// Create and Patch run in the same test, almost always the same wall-clock
// second, so a real (bad) bump to time.Now() would still read as "unchanged".
func TestApplyMeetingPatchNoopWritesNothing(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{Title: "IC weekly", Status: db.MeetingProposed})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	const oldUpdatedAt = 1_000_000
	if _, err := st.DB.Exec(`UPDATE meetings SET updated_at = ? WHERE id = ?`, oldUpdatedAt, m.ID); err != nil {
		t.Fatalf("seed old updated_at: %v", err)
	}

	sameStatus := db.MeetingProposed
	changes, err := st.ApplyMeetingPatch(m.ID, db.MeetingPatch{
		Status: &sameStatus, Source: db.ChangeByModel, Note: "no real change",
	})
	if err != nil {
		t.Fatalf("ApplyMeetingPatch: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("got %d changes, want 0 for an equal value", len(changes))
	}

	after, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if after.UpdatedAt != oldUpdatedAt {
		t.Errorf("updated_at moved from %d to %d with no real change", oldUpdatedAt, after.UpdatedAt)
	}
}

// TestApplyMeetingPatchUserWins is decision 6 from the design: an edit the
// user made by hand beats a model patch based on an older message, but a
// model patch based on a NEWER message (said after the user's edit) still
// goes through.
func TestApplyMeetingPatchUserWins(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{Title: "Kickoff", StartsAt: 1_000_000_000})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	userStarts := int64(2_000_000_000)
	before := &db.Meeting{ID: m.ID, StartsAt: m.StartsAt}
	after := &db.Meeting{ID: m.ID, StartsAt: userStarts}
	// The user edit is not applied through ApplyMeetingPatch (RecordMeetingEdit
	// only writes the trail), so set the column directly first.
	if _, err := st.DB.Exec(`UPDATE meetings SET starts_at = ? WHERE id = ?`, userStarts, m.ID); err != nil {
		t.Fatalf("seed user edit: %v", err)
	}
	if err := st.RecordMeetingEdit(before, after, db.ChangeByUser); err != nil {
		t.Fatalf("RecordMeetingEdit: %v", err)
	}
	userEditAt := time.Now().Unix()

	// A model patch whose evidence predates the user's edit must not move
	// starts_at.
	modelStarts := int64(3_000_000_000)
	changes, err := st.ApplyMeetingPatch(m.ID, db.MeetingPatch{
		StartsAt: &modelStarts, Source: db.ChangeByModel, Note: "old message",
		EvidenceTS: userEditAt - 3600,
	})
	if err != nil {
		t.Fatalf("ApplyMeetingPatch (older evidence): %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("an older model patch overwrote the user's edit: %+v", changes)
	}
	got, _ := st.GetMeeting(m.ID)
	if got.StartsAt != userStarts {
		t.Fatalf("starts_at = %d, want the user's value %d to survive", got.StartsAt, userStarts)
	}

	// A model patch whose evidence is NEWER than the user's edit must go
	// through: a fresh message overrides a stale manual edit.
	newerModelStarts := int64(4_000_000_000)
	changes, err = st.ApplyMeetingPatch(m.ID, db.MeetingPatch{
		StartsAt: &newerModelStarts, Source: db.ChangeByModel, Note: "new message",
		EvidenceTS: userEditAt + 3600,
	})
	if err != nil {
		t.Fatalf("ApplyMeetingPatch (newer evidence): %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1 for a newer model patch", len(changes))
	}
	got, _ = st.GetMeeting(m.ID)
	if got.StartsAt != newerModelStarts {
		t.Fatalf("starts_at = %d, want the newer model patch %d to win", got.StartsAt, newerModelStarts)
	}
}

// TestApplyMeetingPatchAgendaDedup checks that AgendaAdd skips a line that
// already exists (case/whitespace-insensitive) and adds a genuinely new one.
func TestApplyMeetingPatchAgendaDedup(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{Title: "Planning"})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if _, err := st.AddMeetingItem(&db.MeetingItem{
		MeetingID: m.ID, Kind: db.ItemAgenda, Text: "Budget review",
	}); err != nil {
		t.Fatalf("AddMeetingItem: %v", err)
	}

	changes, err := st.ApplyMeetingPatch(m.ID, db.MeetingPatch{
		AgendaAdd: []string{"  budget REVIEW  ", "Hiring plan"},
		Source:    db.ChangeByModel, Note: "agenda grew",
	})
	if err != nil {
		t.Fatalf("ApplyMeetingPatch: %v", err)
	}
	if len(changes) != 1 || changes[0].Field != "agenda" || changes[0].NewValue != "Hiring plan" {
		t.Fatalf("got %+v, want one agenda change for 'Hiring plan'", changes)
	}

	items, err := st.MeetingItems(m.ID)
	if err != nil {
		t.Fatalf("MeetingItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d agenda items, want 2 (the duplicate must not be added)", len(items))
	}
}

// TestMeetingFollowUpsForChat covers the follow-up window: a later message in
// the meeting's chat makes it show up, checking past that message removes it,
// and a message past the window's end does not count.
func TestMeetingFollowUpsForChat(t *testing.T) {
	st := newTestStore(t)
	chat := "amer@s.whatsapp.net"

	base := int64(1_700_000_000)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "Undated catch-up", OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	seedChatMessage(t, st, chat, "origin", base)
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "origin"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}

	now := base + 1000
	fus, err := st.MeetingFollowUpsForChat(chat, now)
	if err != nil {
		t.Fatalf("MeetingFollowUpsForChat: %v", err)
	}
	if len(fus) != 0 {
		t.Fatalf("no new message yet, got %d follow-ups", len(fus))
	}

	// A later message in the chat makes the meeting due for a re-read.
	seedChatMessage(t, st, chat, "later", base+500)
	fus, err = st.MeetingFollowUpsForChat(chat, now)
	if err != nil {
		t.Fatalf("MeetingFollowUpsForChat: %v", err)
	}
	if len(fus) != 1 || fus[0].Meeting.ID != m.ID {
		t.Fatalf("got %+v, want the meeting to show up (a newer message exists)", fus)
	}

	// Checked past that message: it drops out.
	if err := st.SetMeetingChecked(m.ID, base+500); err != nil {
		t.Fatalf("SetMeetingChecked: %v", err)
	}
	fus, err = st.MeetingFollowUpsForChat(chat, now)
	if err != nil {
		t.Fatalf("MeetingFollowUpsForChat: %v", err)
	}
	if len(fus) != 0 {
		t.Fatalf("got %d follow-ups after checking past the message, want 0", len(fus))
	}

	// A message after the follow-up window's end (21 days past the anchor,
	// since this meeting has no date) does not count.
	farFuture := base + (db.MeetingFollowUndatedDays+5)*86400
	seedChatMessage(t, st, chat, "too-late", farFuture)
	fus, err = st.MeetingFollowUpsForChat(chat, farFuture+100)
	if err != nil {
		t.Fatalf("MeetingFollowUpsForChat: %v", err)
	}
	if len(fus) != 0 {
		t.Fatalf("got %d follow-ups for a message past the window, want 0: %+v", len(fus), fus)
	}
}

// TestCloseStaleMeetings covers the three closing rules, and the two ways a
// meeting is left alone: an unread follow-up, or a status the user set.
func TestCloseStaleMeetings(t *testing.T) {
	st := newTestStore(t)
	now := int64(1_700_000_000)
	dayAgo := now - 24*3600 - 10

	// Rule 1: confirmed + dated + date passed by 24h -> held.
	confirmed, err := st.CreateMeeting(&db.Meeting{
		Title: "Confirmed and past", Status: db.MeetingConfirmed, StartsAt: dayAgo,
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	// Rule 2: proposed + dated + date passed by 24h -> lapsed.
	proposed, err := st.CreateMeeting(&db.Meeting{
		Title: "Proposed and past", Status: db.MeetingProposed, StartsAt: dayAgo,
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	// Rule 3: undated, chat quiet for 21+ days -> lapsed. CreateMeeting sets
	// created_at to time.Now(), which is far after `now`, so back it up by
	// hand through direct SQL (the anchor falls back to created_at).
	undated, err := st.CreateMeeting(&db.Meeting{Title: "Undated and quiet"})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	oldCreated := now - (db.MeetingFollowUndatedDays+1)*86400
	if _, err := st.DB.Exec(`UPDATE meetings SET created_at = ? WHERE id = ?`, oldCreated, undated.ID); err != nil {
		t.Fatalf("seed old created_at: %v", err)
	}

	// Left alone: has an unread follow-up (a new chat message waiting).
	unread, err := st.CreateMeeting(&db.Meeting{
		Title: "Has a new message", Status: db.MeetingProposed, StartsAt: dayAgo,
		OriginChatJID: "unread@s.whatsapp.net", OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	seedChatMessage(t, st, "unread@s.whatsapp.net", "origin", now-100000)
	if err := st.LinkMeetingMessage(unread.ID, "unread@s.whatsapp.net", "origin", "origin"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	seedChatMessage(t, st, "unread@s.whatsapp.net", "fresh", now-10)

	// Left alone: the user set its status by hand.
	userSet, err := st.CreateMeeting(&db.Meeting{
		Title: "User confirmed it", Status: db.MeetingConfirmed, StartsAt: dayAgo,
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.RecordMeetingEdit(
		&db.Meeting{ID: userSet.ID, Status: db.MeetingProposed},
		&db.Meeting{ID: userSet.ID, Status: db.MeetingConfirmed},
		db.ChangeByUser,
	); err != nil {
		t.Fatalf("RecordMeetingEdit: %v", err)
	}

	changes, err := st.CloseStaleMeetings(now)
	if err != nil {
		t.Fatalf("CloseStaleMeetings: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want 3 (one per closed meeting)", len(changes))
	}

	got, _ := st.GetMeeting(confirmed.ID)
	if got.Status != db.MeetingHeld {
		t.Errorf("confirmed+dated+past = %q, want held", got.Status)
	}
	got, _ = st.GetMeeting(proposed.ID)
	if got.Status != db.MeetingLapsed {
		t.Errorf("proposed+dated+past = %q, want lapsed", got.Status)
	}
	got, _ = st.GetMeeting(undated.ID)
	if got.Status != db.MeetingLapsed {
		t.Errorf("undated+quiet = %q, want lapsed", got.Status)
	}
	got, _ = st.GetMeeting(unread.ID)
	if got.Status != db.MeetingProposed {
		t.Errorf("a meeting with an unread follow-up should be left alone, got %q", got.Status)
	}
	got, _ = st.GetMeeting(userSet.ID)
	if got.Status != db.MeetingConfirmed {
		t.Errorf("a user-set status should be left alone, got %q", got.Status)
	}
}

// TestUpcomingExcludesLapsed checks that ListMeetings(Upcoming) drops a
// lapsed meeting the same way it already drops held/cancelled ones.
func TestUpcomingExcludesLapsed(t *testing.T) {
	st := newTestStore(t)

	future := time.Now().Unix() + 3*86400
	if _, err := st.CreateMeeting(&db.Meeting{Title: "Ahead", StartsAt: future, Status: db.MeetingConfirmed}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if _, err := st.CreateMeeting(&db.Meeting{Title: "Dead", StartsAt: future, Status: db.MeetingLapsed}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	got, err := st.ListMeetings(db.MeetingFilter{Upcoming: true})
	if err != nil {
		t.Fatalf("ListMeetings: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Ahead" {
		t.Fatalf("got %v, want only the non-lapsed meeting", got)
	}
}

// TestUpcomingMeetingChatJIDsExcludesLapsed is the second place "upcoming"
// filters: a chat whose only meeting is lapsed must not stay in the chat-list
// filter forever, same as the ListMeetings check above.
func TestUpcomingMeetingChatJIDsExcludesLapsed(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.CreateMeeting(&db.Meeting{
		Title: "Dead", Status: db.MeetingLapsed,
		OriginChatJID: "lapsed@g.us", OriginMessageID: "m1",
	}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	got, err := st.UpcomingMeetingChatJIDs()
	if err != nil {
		t.Fatalf("UpcomingMeetingChatJIDs: %v", err)
	}
	if got["lapsed@g.us"] {
		t.Errorf("a chat whose only meeting is lapsed must drop out of the meetings filter")
	}
}

// TestCloseStaleMeetingsClosesMeetingInArchivedChat is the bug the review
// found: a chat the AI is excluded from (hidden, archived, deleted) is never
// read by the updater, so checked_ts for a meeting that only lives in that
// chat never moves. Before the fix, meetingHasUnreadFollowUp treated a
// message sitting in that chat as "unread" forever, so CloseStaleMeetings
// skipped the meeting for good and it never lapsed.
func TestCloseStaleMeetingsClosesMeetingInArchivedChat(t *testing.T) {
	st := newTestStore(t)
	now := int64(1_700_000_000)
	chat := "archived@s.whatsapp.net"

	if err := st.StoreChat(&db.Chat{JID: chat, IsArchived: true}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if !st.IsChatExcludedFromAI(chat) {
		t.Fatalf("test setup: an archived chat should be excluded from AI")
	}

	m, err := st.CreateMeeting(&db.Meeting{
		Title: "Quiet in an archived chat", OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	oldCreated := now - (db.MeetingFollowUndatedDays+1)*86400
	if _, err := st.DB.Exec(`UPDATE meetings SET created_at = ? WHERE id = ?`, oldCreated, m.ID); err != nil {
		t.Fatalf("seed old created_at: %v", err)
	}
	seedChatMessage(t, st, chat, "origin", oldCreated)
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "origin"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	// A "new" message in the archived chat. Without the exclusion, this reads
	// as an unread follow-up and the meeting would never lapse.
	seedChatMessage(t, st, chat, "fresh", now-10)

	changes, err := st.CloseStaleMeetings(now)
	if err != nil {
		t.Fatalf("CloseStaleMeetings: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingLapsed {
		t.Errorf("a meeting whose only chat is archived should still lapse, got %q", got.Status)
	}
}

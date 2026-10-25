package db_test

import (
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// TestMeetingLinkCode covers the join key that ties one meeting together across
// chats. Real messages carry the link in several shapes: bare, with a query
// string, or wrapped in Arabic text.
func TestMeetingLinkCode(t *testing.T) {
	cases := map[string]string{
		"https://meet.google.com/vfd-dpuf-sgv":              "vfd-dpuf-sgv",
		"https://meet.google.com/vfd-dpuf-sgv?authuser=0":   "vfd-dpuf-sgv",
		"يلا بانتظارك https://meet.google.com/oct-jooz-ygq": "oct-jooz-ygq",
		"meet.google.com/yds-ozaw-fvk":                      "yds-ozaw-fvk",
		"https://zoom.us/j/98765432100":                     "zoom:98765432100",
		"no link here at all":                               "",
		"https://meet.google.com/short":                     "",
	}
	for in, want := range cases {
		if got := db.MeetingLinkCode(in); got != want {
			t.Errorf("MeetingLinkCode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMeetingSpansChats is the case a calendar cannot hold: one meeting
// arranged through three separate DMs, with no group anywhere. The link code
// is what proves the second and third chat are the same meeting.
func TestMeetingSpansChats(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{
		Title:           "Flow OS review",
		Link:            "https://meet.google.com/vws-sdwj-zzc",
		OriginChatJID:   "amer@s.whatsapp.net",
		OriginMessageID: "m1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if m.LinkCode != "vws-sdwj-zzc" {
		t.Fatalf("link_code = %q, want the join code filled in from the link", m.LinkCode)
	}

	// The same link turns up in two other DMs.
	found, err := st.FindMeetingByCode(db.MeetingLinkCode("https://meet.google.com/vws-sdwj-zzc?authuser=0"))
	if err != nil {
		t.Fatalf("FindMeetingByCode: %v", err)
	}
	if found == nil || found.ID != m.ID {
		t.Fatalf("the same link in another chat must resolve to the existing meeting")
	}
	for _, chat := range []string{"ayman@s.whatsapp.net", "fady@s.whatsapp.net"} {
		if err := st.LinkMeetingMessage(m.ID, chat, "x", "invite"); err != nil {
			t.Fatalf("LinkMeetingMessage(%s): %v", chat, err)
		}
	}

	chats, err := st.MeetingChatJIDs(m.ID)
	if err != nil {
		t.Fatalf("MeetingChatJIDs: %v", err)
	}
	if len(chats) != 3 {
		t.Fatalf("meeting spans %d chats, want 3: %v", len(chats), chats)
	}
}

// TestMeetingParticipantsKeepKnownFacts guards the merge rule: someone
// mentioned again in a second chat must not lose an RSVP already recorded.
func TestMeetingParticipantsKeepKnownFacts(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{Title: "IC weekly"})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	jid := "amer@s.whatsapp.net"
	if err := st.AddMeetingParticipant(m.ID, db.MeetingParticipant{
		JID: jid, Name: "Amer", RSVP: "no", Note: "عنده ميتنج مع الفاوندرز",
	}); err != nil {
		t.Fatalf("AddMeetingParticipant: %v", err)
	}
	// Seen again elsewhere, this time with nothing new known.
	if err := st.AddMeetingParticipant(m.ID, db.MeetingParticipant{JID: jid}); err != nil {
		t.Fatalf("AddMeetingParticipant (second): %v", err)
	}

	ps, err := st.MeetingParticipants(m.ID)
	if err != nil {
		t.Fatalf("MeetingParticipants: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("got %d participants, want 1 (same person, not a duplicate)", len(ps))
	}
	if ps[0].RSVP != "no" || ps[0].Name != "Amer" || ps[0].Note == "" {
		t.Errorf("a later mention wiped what was already known: %+v", ps[0])
	}
}

// TestMeetingItems checks agenda, requirements and next steps sharing one
// table, and that a next step can be promoted to a task.
func TestMeetingItems(t *testing.T) {
	st := newTestStore(t)

	m, _ := st.CreateMeeting(&db.Meeting{Title: "MinX platform"})
	for _, it := range []db.MeetingItem{
		{MeetingID: m.ID, Kind: db.ItemAgenda, Text: "Kateb SEO walkthrough"},
		{MeetingID: m.ID, Kind: db.ItemRequirement, Text: "Flow OS update at 80%+"},
		{MeetingID: m.ID, Kind: db.ItemNextStep, Text: "Send the contract"},
	} {
		copy := it
		if _, err := st.AddMeetingItem(&copy); err != nil {
			t.Fatalf("AddMeetingItem(%s): %v", it.Kind, err)
		}
	}

	items, err := st.MeetingItems(m.ID)
	if err != nil {
		t.Fatalf("MeetingItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}

	// Promote the next step into the tasks module.
	task, err := st.CreateTask(&db.Task{Title: "Send the contract"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	var next *db.MeetingItem
	for i := range items {
		if items[i].Kind == db.ItemNextStep {
			next = &items[i]
		}
	}
	if next == nil {
		t.Fatal("no next_step item found")
	}
	next.TaskID = &task.ID
	next.Done = true
	if err := st.UpdateMeetingItem(next); err != nil {
		t.Fatalf("UpdateMeetingItem: %v", err)
	}

	items, _ = st.MeetingItems(m.ID)
	for _, it := range items {
		if it.Kind != db.ItemNextStep {
			continue
		}
		if it.TaskID == nil || *it.TaskID != task.ID {
			t.Errorf("next step did not keep its task link")
		}
		if !it.Done {
			t.Errorf("next step should be done once promoted")
		}
	}
}

// TestListMeetingsOrder checks the queue order: meetings with no agreed time
// come first, because those are the ones waiting on a decision.
func TestListMeetingsOrder(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.CreateMeeting(&db.Meeting{Title: "Fixed", StartsAt: 2_000_000_000, Status: db.MeetingConfirmed}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if _, err := st.CreateMeeting(&db.Meeting{Title: "Still arguing", TimeOptions: `["Sat 17:00","Sun 20:30"]`}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	got, err := st.ListMeetings(db.MeetingFilter{})
	if err != nil {
		t.Fatalf("ListMeetings: %v", err)
	}
	if len(got) != 2 || got[0].Title != "Still arguing" {
		t.Fatalf("undated meetings must sort first, got %v", []string{got[0].Title, got[1].Title})
	}
}

// TestMeetingMapLink covers the offline location. Real chats carry maps.app.goo.gl
// short links, often with a ?g_st= query string and sometimes no scheme, and
// never the long google.com/maps form.
func TestMeetingMapLink(t *testing.T) {
	cases := map[string]string{
		"https://maps.app.goo.gl/7YL4vUViZtCYFq916":         "https://maps.app.goo.gl/7YL4vUViZtCYFq916",
		"https://maps.app.goo.gl/dxgqN6sg4F8EGMFd8?g_st=iw": "https://maps.app.goo.gl/dxgqN6sg4F8EGMFd8?g_st=iw",
		"المكتب هنا https://maps.app.goo.gl/abc123 تفضلوا":  "https://maps.app.goo.gl/abc123",
		"maps.app.goo.gl/noScheme":                          "https://maps.app.goo.gl/noScheme",
		"مكتبنا بجاده ٣٠":                                   "",
	}
	for in, want := range cases {
		if got := db.MeetingMapLink(in); got != want {
			t.Errorf("MeetingMapLink(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMeetingOfflineLocation checks the three shapes a place arrives in living
// together: free text, a maps link, and a native WhatsApp pin's coordinates.
func TestMeetingOfflineLocation(t *testing.T) {
	st := newTestStore(t)

	m, err := st.CreateMeeting(&db.Meeting{
		Title:       "ORBIT leadership, in person",
		Mode:        "in_person",
		Location:    "مكتبنا بجاده ٣٠ https://maps.app.goo.gl/yJseT9NAiD4FSrHD9?g_st=ic",
		LocationLat: 21.6606254577637,
		LocationLng: 39.1729354858398,
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if m.LocationURL != "https://maps.app.goo.gl/yJseT9NAiD4FSrHD9?g_st=ic" {
		t.Errorf("a maps link in the location text should become location_url, got %q", m.LocationURL)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.LocationLat == 0 || got.LocationLng == 0 {
		t.Errorf("coordinates from a WhatsApp location pin were not kept: %+v", got)
	}
	if got.Mode != "in_person" {
		t.Errorf("mode = %q, want in_person", got.Mode)
	}
}

// TestMeetingInheritsCircles checks that a meeting is filed automatically: it
// takes the circles of the group it was arranged in and of the people in it,
// including when a person is filed under a different identity form than the one
// the meeting names.
func TestMeetingInheritsCircles(t *testing.T) {
	st := newTestStore(t)

	studio, err := st.CreateCircle("NorthStudio", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	minx, err := st.CreateCircle("MinX", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}
	other, err := st.CreateCircle("Unrelated", "", "")
	if err != nil {
		t.Fatalf("CreateCircle: %v", err)
	}

	group := "studio@g.us"
	if err := st.AddCircleMember(studio.ID, db.MemberGroup, group); err != nil {
		t.Fatalf("AddCircleMember(group): %v", err)
	}
	// This person is filed by phone JID, but the meeting will name their @lid.
	phone := "966500@s.whatsapp.net"
	lid := "77700@lid"
	if err := st.StoreContact(&db.Contact{JID: phone, LID: lid, Name: "Nabil"}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	if err := st.AddCircleMember(minx.ID, db.MemberContact, phone); err != nil {
		t.Fatalf("AddCircleMember(contact): %v", err)
	}

	m, err := st.CreateMeeting(&db.Meeting{
		Title:           "Platform review",
		OriginChatJID:   group,
		OriginMessageID: "m1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.AddMeetingParticipant(m.ID, db.MeetingParticipant{JID: lid, Name: "Nabil"}); err != nil {
		t.Fatalf("AddMeetingParticipant: %v", err)
	}

	got, err := st.MeetingCircles(m.ID)
	if err != nil {
		t.Fatalf("MeetingCircles: %v", err)
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["NorthStudio"] {
		t.Errorf("meeting should inherit the circle of the group it was arranged in")
	}
	if !names["MinX"] {
		t.Errorf("meeting should inherit a participant's circle across the LID/phone split")
	}
	if names["Unrelated"] {
		t.Errorf("meeting picked up a circle nothing connects it to")
	}
	_ = other

	// Dropping the person drops the circle they brought.
	if err := st.RemoveMeetingParticipant(m.ID, lid); err != nil {
		t.Fatalf("RemoveMeetingParticipant: %v", err)
	}
	got, _ = st.MeetingCircles(m.ID)
	for _, c := range got {
		if c.Name == "MinX" {
			t.Errorf("removing the only MinX person should drop that circle")
		}
	}
}

// TestMeetingOriginTS: a meeting found today from a conversation held months
// ago must report when the conversation happened, not when it was found.
func TestMeetingOriginTS(t *testing.T) {
	st := newTestStore(t)
	chat := "9055@s.whatsapp.net"
	said := time.Date(2026, 6, 5, 15, 0, 0, 0, time.UTC)
	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Karim"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if err := st.StoreMessage(&db.Message{ID: "m1", ChatJID: chat, Sender: chat,
		Content: "لازم نجتمع قريب", Timestamp: said.Unix()}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	m, err := st.CreateMeeting(&db.Meeting{Title: "Catch up", OriginChatJID: chat, OriginMessageID: "m1"})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	list, err := st.ListMeetings(db.MeetingFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("ListMeetings: %v (%d)", err, len(list))
	}
	if list[0].OriginTS != said.Unix() {
		t.Errorf("list origin_ts = %d, want the message time %d", list[0].OriginTS, said.Unix())
	}
	got, _ := st.GetMeeting(m.ID)
	if got.OriginTS != said.Unix() {
		t.Errorf("get origin_ts = %d, want %d", got.OriginTS, said.Unix())
	}
}

// TestMeetingCirclesWeighPeople is the "one person in every circle" case. A
// person spread across many circles must not stamp all of them on a meeting;
// a person in one circle, or the circle's name in the title, still decides it.
func TestMeetingCirclesWeighPeople(t *testing.T) {
	st := newTestStore(t)

	// Karim sits in five circles. Sami sits in one.
	alaa := "905500@s.whatsapp.net"
	sami := "974500@s.whatsapp.net"
	for _, name := range []string{"Acme", "Borealis", "ZED", "Souq", "Orbit"} {
		c, err := st.CreateCircle(name, "", "")
		if err != nil {
			t.Fatalf("CreateCircle(%s): %v", name, err)
		}
		if err := st.AddCircleMember(c.ID, db.MemberContact, alaa); err != nil {
			t.Fatalf("AddCircleMember: %v", err)
		}
	}
	minx, _ := st.CreateCircle("MinX", "", "")
	if err := st.AddCircleMember(minx.ID, db.MemberContact, sami); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}
	qrt, _ := st.CreateCircle("QRT", "", "")
	if err := st.UpdateCircle(qrt.ID, "QRT", "", "", []string{"Quarterly Review Team"}); err != nil {
		t.Fatalf("UpdateCircle: %v", err)
	}

	circlesOf := func(title string, people ...string) map[string]bool {
		t.Helper()
		// Arranged in a DM with Karim, and Karim is also listed as a participant:
		// the same person twice, who must still only vote once.
		m, err := st.CreateMeeting(&db.Meeting{Title: title, OriginChatJID: alaa, OriginMessageID: title})
		if err != nil {
			t.Fatalf("CreateMeeting: %v", err)
		}
		for _, p := range append(people, alaa) {
			if err := st.AddMeetingParticipant(m.ID, db.MeetingParticipant{JID: p}); err != nil {
				t.Fatalf("AddMeetingParticipant: %v", err)
			}
		}
		got, err := st.MeetingCircles(m.ID)
		if err != nil {
			t.Fatalf("MeetingCircles: %v", err)
		}
		names := map[string]bool{}
		for _, c := range got {
			names[c.Name] = true
		}
		return names
	}

	if got := circlesOf("Quick decisions"); len(got) != 0 {
		t.Errorf("a meeting with only a many-circle person should get no circle, got %v", got)
	}
	if got := circlesOf("Acme Meeting"); len(got) != 1 || !got["Acme"] {
		t.Errorf("the circle named in the title should be the only one, got %v", got)
	}
	if got := circlesOf("Next steps", sami); len(got) != 1 || !got["MinX"] {
		t.Errorf("a one-circle person should decide the circle, got %v", got)
	}
	if got := circlesOf("مناقشة قصة الـQRT مع الشركة"); !got["QRT"] {
		t.Errorf("a Latin name glued to an Arabic article should still match, got %v", got)
	}
	if got := circlesOf("Visit to the quarterly review team"); !got["QRT"] {
		t.Errorf("a circle keyword should match, got %v", got)
	}
	if got := circlesOf("Zedric launch party"); got["ZED"] {
		t.Errorf("a circle name must match whole words only, got %v", got)
	}
}

// TestMeetingCirclesIgnoreOwner: the account owner is in every meeting, so the
// circles they are filed under must not land on all of them — also when the
// meeting names their @lid and the contact row stores the LID bare.
func TestMeetingCirclesIgnoreOwner(t *testing.T) {
	st := newTestStore(t)

	me := "966500@s.whatsapp.net"
	if err := st.StoreContact(&db.Contact{JID: me, LID: "63800", Name: "Me"}); err != nil {
		t.Fatalf("StoreContact: %v", err)
	}
	zed, _ := st.CreateCircle("ZED", "", "")
	if err := st.AddCircleMember(zed.ID, db.MemberContact, "63800@lid"); err != nil {
		t.Fatalf("AddCircleMember: %v", err)
	}

	m, err := st.CreateMeeting(&db.Meeting{Title: "Weekly sync"})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.AddMeetingParticipant(m.ID, db.MeetingParticipant{JID: me}); err != nil {
		t.Fatalf("AddMeetingParticipant: %v", err)
	}
	if got, _ := st.MeetingCircles(m.ID); len(got) != 1 {
		t.Fatalf("before the owner is known they vote like anyone, got %v", got)
	}

	if err := st.PutSyncState(db.OwnJIDKey, me); err != nil {
		t.Fatalf("PutSyncState: %v", err)
	}
	if err := st.SyncMeetingCircles(m.ID); err != nil {
		t.Fatalf("SyncMeetingCircles: %v", err)
	}
	if got, _ := st.MeetingCircles(m.ID); len(got) != 0 {
		t.Errorf("the owner's circles should not be inherited, got %v", got)
	}
}

// TestUpcomingMeetingChatJIDs backs the "Meetings" filter in the chat list: a
// chat qualifies through the arrangement trail OR through someone attending,
// and a meeting still being negotiated counts because it is the one that needs
// a decision. Held, cancelled and rejected meetings drop out.
func TestUpcomingMeetingChatJIDs(t *testing.T) {
	st := newTestStore(t)
	soon := time.Now().Unix() + 3*86400

	// Arranged in a group, still ahead.
	ahead, err := st.CreateMeeting(&db.Meeting{
		Title: "Ahead", Status: db.MeetingConfirmed, StartsAt: soon,
		OriginChatJID: "live@g.us", OriginMessageID: "m1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	// Someone attending whose own DM carries no meeting message.
	if err := st.AddMeetingParticipant(ahead.ID, db.MeetingParticipant{JID: "amer@s.whatsapp.net"}); err != nil {
		t.Fatalf("AddMeetingParticipant: %v", err)
	}

	// No time agreed yet — still needs a decision, so it counts.
	if _, err := st.CreateMeeting(&db.Meeting{
		Title: "Being arranged", OriginChatJID: "arguing@g.us", OriginMessageID: "m2",
	}); err != nil {
		t.Fatalf("CreateMeeting(undated): %v", err)
	}

	// Already held — must not keep a chat in the filter forever.
	if _, err := st.CreateMeeting(&db.Meeting{
		Title: "Done", Status: db.MeetingHeld, StartsAt: time.Now().Unix() - 10*86400,
		OriginChatJID: "past@g.us", OriginMessageID: "m3",
	}); err != nil {
		t.Fatalf("CreateMeeting(held): %v", err)
	}

	got, err := st.UpcomingMeetingChatJIDs()
	if err != nil {
		t.Fatalf("UpcomingMeetingChatJIDs: %v", err)
	}
	for _, want := range []string{"live@g.us", "amer@s.whatsapp.net", "arguing@g.us"} {
		if !got[want] {
			t.Errorf("%s should be in the meetings filter", want)
		}
	}
	if got["past@g.us"] {
		t.Errorf("a chat whose only meeting is already held must drop out")
	}
}

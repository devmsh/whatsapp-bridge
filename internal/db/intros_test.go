package db_test

import (
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// TestIntroChats covers the "someone I just met" view: a recently-started chat
// whose opening reads like an introduction. It also guards the SQLite gotcha
// that made the first version of this query silently return nothing —
// min(timestamp) is an aggregate with no column affinity, so comparing it to a
// string does a TEXT comparison and matches no rows at all.
func TestIntroChats(t *testing.T) {
	st := newTestStore(t)

	now := time.Now().Unix()
	recent := now - 10*86400
	old := now - 300*86400

	mk := func(jid, name string, first int64, texts ...string) {
		if err := st.StoreChat(&db.Chat{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreChat(%s): %v", jid, err)
		}
		if err := st.StoreContact(&db.Contact{JID: jid, Name: name}); err != nil {
			t.Fatalf("StoreContact(%s): %v", jid, err)
		}
		for i, txt := range texts {
			if err := st.StoreMessage(&db.Message{
				ID: jid + "-" + string(rune('a'+i)), ChatJID: jid,
				Content: txt, Timestamp: first + int64(i),
			}); err != nil {
				t.Fatalf("StoreMessage: %v", err)
			}
		}
	}

	// A real introduction: they say who they are and suggest meeting.
	mk("intro@s.whatsapp.net", "Ghiyath", recent,
		"أهلا محمد", "غياث العاني معك، عبدالله عيطه أعطاني رقمك واقترح نرتب لقاء")
	// Recent, but nothing introductory about it.
	mk("errand@s.whatsapp.net", "Driver", recent, "I'm outside", "Order hanger")
	// Introductory words, but the relationship is old — not a new intro.
	mk("oldfriend@s.whatsapp.net", "Old Friend", old,
		"تشرفت بمعرفتك", "نرتب لقاء قريب")
	// A recent intro that is hidden must never surface.
	mk("private@s.whatsapp.net", "Private", recent, "معك سالم، تشرفت بمعرفتك")
	if err := st.HideChat("private@s.whatsapp.net"); err != nil {
		t.Fatalf("HideChat: %v", err)
	}

	since := now - 90*86400
	got, err := st.IntroChats(since, 40, 0)
	if err != nil {
		t.Fatalf("IntroChats: %v", err)
	}
	if len(got) != 1 {
		names := make([]string, len(got))
		for i, c := range got {
			names[i] = c.Name
		}
		t.Fatalf("IntroChats = %v, want only [Ghiyath]", names)
	}
	if got[0].JID != "intro@s.whatsapp.net" {
		t.Fatalf("got %s, want the introduction", got[0].JID)
	}
	if got[0].Score < 3 {
		t.Errorf("score = %d, want the referral and meet-soon signals counted", got[0].Score)
	}
	if len(got[0].Signals) == 0 {
		t.Errorf("signals should explain why it matched")
	}
}

// TestIntroChatsRespectsMessageCap keeps the view to relationships that have
// not yet turned into real work.
func TestIntroChatsRespectsMessageCap(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().Unix()
	jid := "busy@s.whatsapp.net"

	if err := st.StoreChat(&db.Chat{JID: jid, Name: "Busy"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	st.StoreMessage(&db.Message{ID: "m0", ChatJID: jid, Content: "معك سالم، تشرفت بمعرفتك", Timestamp: now - 5*86400})
	for i := 1; i < 12; i++ {
		st.StoreMessage(&db.Message{
			ID: "m" + string(rune('a'+i)), ChatJID: jid,
			Content: "ongoing work", Timestamp: now - 5*86400 + int64(i),
		})
	}

	got, err := st.IntroChats(now-90*86400, 5, 0)
	if err != nil {
		t.Fatalf("IntroChats: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a chat past the message cap is a working relationship, not an intro: %+v", got)
	}
}

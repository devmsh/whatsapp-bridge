package db_test

import (
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// TestAutoClassifyRespectsTheWatermark is the rule that makes rejection stick.
//
// The first pass over history was reviewed by hand: some suggestions were
// taken, some deliberately were not. If the classifier could reach back over
// those chats it would re-apply a label that was consciously left off, and
// there would be no way to say no permanently.
func TestAutoClassifyRespectsTheWatermark(t *testing.T) {
	st := newTestStore(t)

	tag, err := st.GetOrCreateTag("Intro", "#f59e0b")
	if err != nil {
		t.Fatalf("GetOrCreateTag: %v", err)
	}
	if err := st.SetIntroTagID(tag.ID); err != nil {
		t.Fatalf("SetIntroTagID: %v", err)
	}

	now := time.Now().Unix()
	watermark := now - 30*86400

	mk := func(jid, name string, first int64, texts ...string) {
		st.StoreChat(&db.Chat{JID: jid, Name: name})
		st.StoreContact(&db.Contact{JID: jid, Name: name})
		for i, txt := range texts {
			st.StoreMessage(&db.Message{
				ID: jid + string(rune('a'+i)), ChatJID: jid,
				Content: txt, Timestamp: first + int64(i),
			})
		}
	}

	// Reviewed during the backfill and deliberately left unlabelled.
	mk("old@s.whatsapp.net", "Rejected Earlier", watermark-10*86400,
		"معك سالم العتيبي", "تشرفت بمعرفتك ونرتب لقاء قريب")
	// Someone met since.
	mk("new@s.whatsapp.net", "Met Last Week", now-5*86400,
		"معك خالد المطيري", "تشرفت بمعرفتك اليوم، نرتب لقاء قريب")

	if err := st.SetIntroWatermark(watermark); err != nil {
		t.Fatalf("SetIntroWatermark: %v", err)
	}
	labelled, err := st.AutoClassifyIntros()
	if err != nil {
		t.Fatalf("AutoClassifyIntros: %v", err)
	}
	if len(labelled) != 1 || labelled[0] != "Met Last Week" {
		t.Fatalf("labelled = %v, want only the chat started after the watermark", labelled)
	}

	// The label drives the filter, and only the new one carries it.
	set, err := st.IntroLabelledJIDs()
	if err != nil {
		t.Fatalf("IntroLabelledJIDs: %v", err)
	}
	if !set["new@s.whatsapp.net"] {
		t.Errorf("the newly labelled chat should be in the filter")
	}
	if set["old@s.whatsapp.net"] {
		t.Errorf("a chat rejected during the backfill must never be re-labelled")
	}

	// Removing the label by hand takes it out, and a second pass must not
	// quietly put it back.
	if err := st.UnassignTag("new@s.whatsapp.net", tag.ID); err != nil {
		t.Fatalf("UnassignTag: %v", err)
	}
	if err := st.SetIntroWatermark(now); err != nil {
		t.Fatalf("SetIntroWatermark: %v", err)
	}
	if _, err := st.AutoClassifyIntros(); err != nil {
		t.Fatalf("AutoClassifyIntros (second): %v", err)
	}
	set, _ = st.IntroLabelledJIDs()
	if set["new@s.whatsapp.net"] {
		t.Errorf("removing a label must stick — the classifier re-applied it")
	}
}

// TestAutoClassifyFirstRunSetsWatermarkOnly guards the boot case: with no
// watermark recorded, the classifier must not sweep all of history.
func TestAutoClassifyFirstRunSetsWatermarkOnly(t *testing.T) {
	st := newTestStore(t)
	tag, _ := st.GetOrCreateTag("Intro", "#f59e0b")
	st.SetIntroTagID(tag.ID)

	jid := "someone@s.whatsapp.net"
	st.StoreChat(&db.Chat{JID: jid, Name: "Someone"})
	st.StoreContact(&db.Contact{JID: jid, Name: "Someone"})
	st.StoreMessage(&db.Message{
		ID: "m1", ChatJID: jid,
		Content: "معك خالد، تشرفت بمعرفتك ونرتب لقاء", Timestamp: time.Now().Unix() - 86400,
	})

	labelled, err := st.AutoClassifyIntros()
	if err != nil {
		t.Fatalf("AutoClassifyIntros: %v", err)
	}
	if len(labelled) != 0 {
		t.Fatalf("the first run must only record a watermark, not label history: %v", labelled)
	}
	if st.IntroWatermark() == 0 {
		t.Fatalf("the first run should have recorded a watermark")
	}
}

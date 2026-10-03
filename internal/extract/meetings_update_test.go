package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Every case here checks the same thing meetings_verify_test.go checks for a
// new meeting: the model may be wrong, and these tests are what say so.

func newUpdateTestStore(t *testing.T) *db.Store {
	t.Helper()
	st, err := db.NewStore(filepath.Join(t.TempDir(), "update-test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedUpdateChatMessage(t *testing.T, st *db.Store, chatJID, id, content string, ts int64) {
	t.Helper()
	if err := st.StoreMessage(&db.Message{
		ID: id, ChatJID: chatJID, Content: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("StoreMessage(%s): %v", id, err)
	}
}

// fakeUpdater answers with whatever it is told to, so RefreshChatMeetings can
// be tested without a model.
type fakeUpdater struct {
	updates []ProposedUpdate
	err     error
	// failOnCall, when not 0, fails only that one call number (1-based) and
	// succeeds on every other — for testing what happens when one chunk's
	// call fails and a later one does not.
	failOnCall int
	calls      int
}

func (f *fakeUpdater) Name() string { return "fake:update-test" }

func (f *fakeUpdater) UpdateMeetings(_ context.Context, _ MeetingUpdateInput) (MeetingUpdateOutput, error) {
	f.calls++
	if f.err != nil {
		return MeetingUpdateOutput{}, f.err
	}
	if f.failOnCall != 0 && f.calls == f.failOnCall {
		return MeetingUpdateOutput{}, errors.New("model unreachable")
	}
	return MeetingUpdateOutput{Updates: f.updates}, nil
}

func TestVerifyUpdateRejections(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	line := func(id, text string) Line {
		return Line{MessageID: id, TS: now, Sender: "Sharif", Text: text}
	}
	undated := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	dated := []KnownMeeting{{
		Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingConfirmed,
		StartsAt: now + 2*24*3600, // two days out — well ahead of "now"
	}}

	cases := []struct {
		name  string
		chunk Chunk
		known []KnownMeeting
		u     ProposedUpdate
		want  UpdateReject
	}{
		{
			name:  "unknown ref",
			chunk: Chunk{Lines: []Line{line("m1", "الغينا الاجتماع")}},
			known: undated,
			u: ProposedUpdate{Ref: 2, Status: "cancelled", EvidenceID: "m1",
				Evidence: "الغينا الاجتماع", Confidence: 0.9},
			want: URejectUnknownRef,
		},
		{
			name:  "not sure enough",
			chunk: Chunk{Lines: []Line{line("m1", "يمكن نأجلها")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, WhenText: "بكرة", EvidenceID: "m1",
				Evidence: "يمكن نأجلها", Confidence: 0.4},
			want: URejectLowScore,
		},
		{
			name:  "names a message that is not here",
			chunk: Chunk{Lines: []Line{line("m1", "الغينا الاجتماع")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "cancelled", EvidenceID: "nope",
				Evidence: "الغينا الاجتماع", Confidence: 0.9},
			want: URejectNoEvidence,
		},
		{
			name:  "quote is not in the message",
			chunk: Chunk{Lines: []Line{line("m1", "خلينا نأكد الوقت")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "cancelled", EvidenceID: "m1",
				Evidence: "الغينا الاجتماع خلاص", Confidence: 0.9},
			want: URejectBadQuote,
		},
		{
			name: "only proof is a carried-in line",
			chunk: Chunk{Lines: []Line{
				{MessageID: "m1", TS: now, Sender: "Sharif", Text: "الغينا الاجتماع", Context: true},
			}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "cancelled", EvidenceID: "m1",
				Evidence: "الغينا الاجتماع", Confidence: 0.9},
			want: URejectContext,
		},
		{
			name:  "not a status this code knows",
			chunk: Chunk{Lines: []Line{line("m1", "خلص الاجتماع اتلغى نهائيا")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "archived", EvidenceID: "m1",
				Evidence: "خلص الاجتماع اتلغى نهائيا", Confidence: 0.9},
			want: URejectBadStatus,
		},
		{
			name:  "nothing in it would change anything",
			chunk: Chunk{Lines: []Line{line("m1", "شكرا للجميع")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, EvidenceID: "m1",
				Evidence: "شكرا للجميع", Confidence: 0.9},
			want: URejectEmpty,
		},
		{
			name: "too early to be held",
			chunk: Chunk{Lines: []Line{
				{MessageID: "m1", TS: now, Sender: "Sharif", Text: "الاجتماع كان تمام"},
			}},
			known: dated,
			u: ProposedUpdate{Ref: 1, Status: "held", EvidenceID: "m1",
				Evidence: "الاجتماع كان تمام", Confidence: 0.9},
			want: URejectTooEarly,
		},
		{
			// Round 2, part B3 / round 4 part 4: a call starting in a few
			// minutes bent a planned meeting on real data ("لقاء الأحد" ->
			// "in 10 minutes").
			name:  "a call starting in a few minutes, not a planned change",
			chunk: Chunk{Lines: []Line{line("m1", "تمام، خلال ١٠ دقايق وبندخل")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, WhenText: "اليوم", TimeText: "خلال ١٠ دقايق", EvidenceID: "m1",
				Evidence: "تمام، خلال ١٠ دقايق وبندخل", Confidence: 0.9},
			want: URejectAdHoc,
		},
		{
			name:  "the evidence itself is an ad-hoc call phrase",
			chunk: Chunk{Lines: []Line{line("m1", "يلا بانتظارك عالاجتماع")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "held", EvidenceID: "m1",
				Evidence: "يلا بانتظارك عالاجتماع", Confidence: 0.9},
			want: URejectAdHoc,
		},
		{
			// Round 2, part B1: "resolved" with no note loses the one thing
			// that makes the status worth recording — what was decided.
			name:  "resolved with no note",
			chunk: Chunk{Lines: []Line{line("m1", "انا شايف انو لازم نجازف")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, Status: "resolved", EvidenceID: "m1",
				Evidence: "انا شايف انو لازم نجازف", Confidence: 0.9},
			want: URejectResolvedNoNote,
		},
		{
			// Round 2, part B4: a timing change needs 0.7, not just the
			// general 0.6 floor.
			name:  "a timing change below the higher timing floor",
			chunk: Chunk{Lines: []Line{line("m1", "خليها بكرة")}},
			known: undated,
			u: ProposedUpdate{Ref: 1, WhenText: "بكرة", EvidenceID: "m1",
				Evidence: "خليها بكرة", Confidence: 0.65},
			want: URejectLowScore,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, reason, ok := VerifyUpdate(c.chunk, c.known, c.u)
			if ok {
				t.Fatalf("should have been dropped as %s, but was kept", c.want)
			}
			if reason != c.want {
				t.Fatalf("dropped as %s, wanted %s", reason, c.want)
			}
		})
	}
}

func TestVerifyUpdateKeepsARealOne(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	chunk := Chunk{Lines: []Line{
		{MessageID: "m1", TS: now, Sender: "Sharif", Text: "الغينا الاجتماع، بنعيد ترتيبه لاحقا"},
	}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, Status: "cancelled", EvidenceID: "m1",
		Evidence: "الغينا الاجتماع، بنعيد ترتيبه لاحقا", Note: "الاجتماع اتلغى", Confidence: 0.9,
	}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	if !ok {
		t.Fatalf("a real update was dropped as %s", reason)
	}
	if v.Known.ID != 42 || v.Line.MessageID != "m1" {
		t.Fatalf("wrong anchoring: %+v", v)
	}
}

// TestVerifyUpdateKeepsAResolvedOneWithANote covers round 2 part B1: a real
// decision made in chat, with a note explaining it, is kept.
func TestVerifyUpdateKeepsAResolvedOneWithANote(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	chunk := Chunk{Lines: []Line{
		{MessageID: "m1", TS: now, Sender: "Sharif", Text: "انا شايف انو لازم نجازف"},
	}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع نقرر فيه", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, Status: "resolved", EvidenceID: "m1", Evidence: "انا شايف انو لازم نجازف",
		Note: "They agreed in chat to take the risk and join.", Confidence: 0.9,
	}
	if _, reason, ok := VerifyUpdate(chunk, known, u); !ok {
		t.Fatalf("a resolved update with a note should be kept, dropped as %s", reason)
	}
}

// TestVerifyUpdateDedupesTimeText covers round 2 part B4: the same word in
// both timing fields must not be read as a specific clock time.
func TestVerifyUpdateDedupesTimeText(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	chunk := Chunk{Lines: []Line{
		{MessageID: "m1", TS: now, Sender: "Sharif", Text: "خلينا الليلة"},
	}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, WhenText: "الليلة", TimeText: "الليلة", EvidenceID: "m1",
		Evidence: "خلينا الليلة", Confidence: 0.9,
	}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	if !ok {
		t.Fatalf("dropped as %s", reason)
	}
	if v.Update.TimeText != "" {
		t.Errorf("time_text duplicating when_text should have been cleared, got %q", v.Update.TimeText)
	}
}

// TestVerifyUpdateAgendaIsFiltered covers round 2 part B2 and round 4 part 1:
// an agenda line that just echoes the evidence, a flood of lines past the
// cap, and (round 4) a line grounded in real words the chat actually used,
// all at once.
func TestVerifyUpdateAgendaIsFiltered(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	evidence := "اكيد حتنشغل شوي مع العيلة بس خلينا نحكي فيها لما نجتمع عن " +
		"الميزانية والمسؤوليات والجدول الزمني والعقد النهائي"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9,
		AgendaAdd: []string{
			evidence, // the model echoing its own evidence back — must be dropped
			"نراجع الميزانية", "نحدد المسؤوليات", "نتفق على الجدول الزمني",
			"نراجع العقد النهائي", // a fifth line, past the cap of 3
		},
	}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	if !ok {
		t.Fatalf("dropped as %s", reason)
	}
	if len(v.Update.AgendaAdd) != 3 {
		t.Fatalf("expected the agenda capped at 3 real lines, got %v", v.Update.AgendaAdd)
	}
	for _, a := range v.Update.AgendaAdd {
		if a == evidence {
			t.Errorf("the evidence quote itself should never survive as an agenda line")
		}
	}
}

// TestVerifyUpdateAgendaOnlyDoesNotSurviveAsEmpty covers the brief's "if that
// leaves the update empty, reject it as empty" rule: an update whose only
// content was an agenda line that filtering removes has nothing left.
func TestVerifyUpdateAgendaOnlyDoesNotSurviveAsEmpty(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	evidence := "اكيد حتنشغل شوي مع العيلة"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9,
		AgendaAdd: []string{evidence}, // the only "point" is the evidence quote itself
	}
	if _, reason, ok := VerifyUpdate(chunk, known, u); ok || reason != URejectEmpty {
		t.Fatalf("an update left with nothing after agenda filtering should be rejected as empty, got ok=%v reason=%s", ok, reason)
	}
}

// The clock-time case from TestResolveMeetingStartClockTimes, reused here:
// "بكرة" + "٥ العصر" sent on a Saturday gives Sunday 17:00.
func TestBuildPatchResolvesWhenAndTime(t *testing.T) {
	sent := time.Date(2026, 9, 12, 10, 0, 0, 0, riyadh).Unix()
	line := Line{MessageID: "m1", TS: sent, Sender: "Sharif", Text: "خليها بكرة الساعة ٥ العصر"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "بكرة", TimeText: "٥ العصر"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, db.Meeting{}, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil {
		t.Fatal("expected a resolved StartsAt")
	}
	d := time.Unix(*p.StartsAt, 0).In(riyadh)
	if d.Day() != 13 || d.Hour() != 17 {
		t.Errorf("got %s, want day 13 hour 17", d.Format("Jan 2 15:04"))
	}
	if p.TimeOptions == nil {
		t.Fatal("expected time_options to be set")
	}
	var words []string
	if err := json.Unmarshal([]byte(*p.TimeOptions), &words); err != nil {
		t.Fatalf("time_options is not valid JSON: %v", err)
	}
	if len(words) != 2 || words[0] != "بكرة" || words[1] != "٥ العصر" {
		t.Errorf("time_options = %v, want [بكرة ٥ العصر]", words)
	}
}

func TestBuildPatchPostponedClearsDate(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "للأسف لازم نأجلها"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{Postponed: true},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	current := db.Meeting{Status: db.MeetingConfirmed, StartsAt: time.Now().Unix() + 3600}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil || *p.StartsAt != 0 {
		t.Fatalf("postponed should clear the date, got %v", p.StartsAt)
	}
	if p.Status == nil || *p.Status != db.MeetingProposed {
		t.Fatalf("postponed should fall back to proposed, got %v", p.Status)
	}
}

func TestBuildPatchConfirmedNeedsADate(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "خلاص متفقين"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{Status: "confirmed"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, db.Meeting{StartsAt: 0}, riyadh, "run-1", "chat@g.us")
	if p.Status == nil || *p.Status != db.MeetingProposed {
		t.Fatalf("confirmed with no date should fall back to proposed, got %v", p.Status)
	}
	if p.StartsAt != nil {
		t.Errorf("no timing was in this update, StartsAt should stay untouched, got %v", *p.StartsAt)
	}
}

// The exact bug TestStaleTimingWordsGetNoDate guards against for the finder:
// "بكرة" said on the 7th, a thread reopened on the 12th, must not land on the
// 13th.
func TestBuildPatchStaleTimingWordsGiveZero(t *testing.T) {
	sep7 := time.Date(2026, 9, 7, 15, 6, 0, 0, riyadh).Unix()
	sep12 := time.Date(2026, 9, 12, 15, 21, 0, 0, riyadh).Unix()
	chunk := Chunk{Lines: []Line{
		{MessageID: "old", TS: sep7, Sender: "Hassan", Text: "بلكي بكرة ان شاء الله"},
		{MessageID: "new", TS: sep12, Sender: "Sharif", Text: "كيف مواعيدكم نرتب اجتماع"},
	}}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "بكرة"},
		Line:   chunk.Lines[1],
		Chunk:  chunk,
	}
	p := BuildPatch(v, db.Meeting{}, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil || *p.StartsAt != 0 {
		t.Fatalf("stale 'بكرة' should give no date, got %v", p.StartsAt)
	}
}

// TestBuildPatchConfirmedNeedsADateEvenWithNoStatusInTheUpdate covers round 2
// part A2: a CONFIRMED meeting that is postponed with no new day loses its
// date, even when the update itself says nothing about status, and the
// effective status (the meeting's current one) must then fall back to
// proposed.
func TestBuildPatchConfirmedNeedsADateEvenWithNoStatusInTheUpdate(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "نأجلها لبعد السفر"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "بعد السفر", Postponed: true}, // real words, names no day
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	current := db.Meeting{Status: db.MeetingConfirmed, StartsAt: time.Now().Unix() + 24*3600}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil || *p.StartsAt != 0 {
		t.Fatalf("a postponement with no new day should clear the date, got %v", p.StartsAt)
	}
	if p.Status == nil || *p.Status != db.MeetingProposed {
		t.Fatalf("a confirmed meeting left with no date must fall back to proposed, got %v", p.Status)
	}
}

// TestBuildPatchKeepsADateWordsCannotReplace is from real data: the model sent
// timing words code could not turn into a day, nobody said "postponed", and
// the agreed date was wiped while the note said the date was confirmed. A date
// already agreed must survive words that replace it with nothing.
func TestBuildPatchKeepsADateWordsCannotReplace(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "تمام زي ما اتفقنا بعد السفر"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "بعد السفر", Status: db.MeetingConfirmed},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	current := db.Meeting{Status: db.MeetingConfirmed, StartsAt: time.Now().Unix() + 24*3600}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt != nil {
		t.Fatalf("the agreed date should be left alone, got %v", *p.StartsAt)
	}
	if p.Status != nil && *p.Status != db.MeetingConfirmed {
		t.Fatalf("the meeting still has its date, so it stays confirmed, got %v", *p.Status)
	}
}

// TestVerifyUpdateTimingMustBeInTheEvidence is from real data: "send the
// feedback by Friday" sat next to a planned meeting and the meeting was moved
// to Friday. Timing words the evidence message does not say are dropped.
func TestVerifyUpdateTimingMustBeInTheEvidence(t *testing.T) {
	lines := []Line{
		{MessageID: "a", TS: 100, Sender: "Karim", Text: "ابعتوا الفيدباك عبر الـ issues مش الواتساب"},
		{MessageID: "b", TS: 200, Sender: "Sami", Text: "خلص حاضر الليلة بنحكي"},
	}
	c := Chunk{Lines: lines}
	known := []KnownMeeting{{Ref: 1, ID: 7, Title: "Acme Meeting", Status: db.MeetingProposed}}

	// Friday is not in message "a": the timing is dropped, and with nothing
	// else in the update it is rejected as empty.
	_, reason, ok := VerifyUpdate(c, known, ProposedUpdate{
		Ref: 1, WhenText: "الجمعة", EvidenceID: "a", Evidence: lines[0].Text, Confidence: 0.9,
	})
	if ok || reason != URejectEmpty {
		t.Errorf("timing the evidence never says should leave an empty update, got ok=%v reason=%q", ok, reason)
	}

	// "الليلة" IS in message "b", digits or not, so it stays.
	v, _, ok := VerifyUpdate(c, known, ProposedUpdate{
		Ref: 1, TimeText: "الليلة", EvidenceID: "b", Evidence: lines[1].Text, Confidence: 0.9,
	})
	if !ok || v.Update.TimeText != "الليلة" {
		t.Errorf("timing said in the evidence must survive, got ok=%v time=%q", ok, v.Update.TimeText)
	}
	if !saidIn("نجتمع الساعة ٥ العصر", "5 العصر") {
		t.Error("Arabic-Indic and Latin digits should match")
	}
}

// TestBuildPatchPostponedDoesNotOverrideAClosingStatus covers round 2 part
// A3: postponed must only force "proposed" when the model did not already
// name a closing status.
func TestBuildPatchPostponedDoesNotOverrideAClosingStatus(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "نأجلها، خلص مش لازم"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{Postponed: true, Status: "cancelled"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	current := db.Meeting{Status: db.MeetingConfirmed, StartsAt: time.Now().Unix() + 3600}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.Status == nil || *p.Status != db.MeetingCancelled {
		t.Fatalf("postponed must not override an explicit cancelled, got %v", p.Status)
	}
	if p.StartsAt == nil || *p.StartsAt != 0 {
		t.Fatalf("the date should still clear, got %v", p.StartsAt)
	}
}

// TestBuildPatchTimeOnlyRefreshesTimeOptions covers round 2 part A5: the
// "keep the day, change the clock" branch must also update the recorded
// words, or time_options keeps saying the old time.
func TestBuildPatchTimeOnlyRefreshesTimeOptions(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif", Text: "خلها ٦ المسا بدل الخميس"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{TimeText: "٦ المسا"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	dated := time.Date(2026, 9, 17, 17, 0, 0, 0, riyadh).Unix() // some day, 5pm
	current := db.Meeting{StartsAt: dated, TimeOptions: `["الخميس","٥ العصر"]`}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil {
		t.Fatal("expected the clock time to move")
	}
	if p.TimeOptions == nil {
		t.Fatal("expected time_options to be refreshed to the new clock time")
	}
	var words []string
	if err := json.Unmarshal([]byte(*p.TimeOptions), &words); err != nil {
		t.Fatalf("time_options is not valid JSON: %v", err)
	}
	if len(words) != 1 || words[0] != "٦ المسا" {
		t.Errorf("time_options = %v, want [٦ المسا] — the stale day word must not survive", words)
	}
}

// TestBuildPatchPartOfDayMovesTheTime is the Sami case from real data: the
// meeting was "today, after Asr" and both sides moved it to "الليلة". The model
// answers with time_text "الليلة" and no day. That names no clock time, but it
// must still move the meeting to the evening of the same day.
func TestBuildPatchPartOfDayMovesTheTime(t *testing.T) {
	sent := time.Date(2026, 9, 18, 17, 43, 0, 0, riyadh).Unix()
	line := Line{MessageID: "m1", TS: sent, Sender: "Sami", Text: "خلص حاضر الليلة بنحكي"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{TimeText: "الليلة"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	noon := time.Date(2026, 9, 18, 12, 0, 0, 0, riyadh).Unix()
	current := db.Meeting{StartsAt: noon, TimeOptions: `["اليوم","بعد العصر"]`}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@s.whatsapp.net")
	if p.StartsAt == nil {
		t.Fatal("\"الليلة\" should move the time")
	}
	got := time.Unix(*p.StartsAt, 0).In(riyadh)
	if got.Day() != 18 || got.Hour() != 21 {
		t.Errorf("starts_at = %s, want 18 Sep 21:00", got.Format("2 Jan 15:04"))
	}
	if p.TimeOptions == nil {
		t.Error("time_options should now say الليلة")
	}
}

// TestBuildPatchTimeOptionsUnchangedIsNotSet covers round 2 part B4: if the
// new timing words are exactly what the meeting already has on record,
// TimeOptions must not be set — nothing actually changed.
func TestBuildPatchTimeOptionsUnchangedIsNotSet(t *testing.T) {
	sent := time.Date(2026, 9, 12, 10, 0, 0, 0, riyadh).Unix()
	line := Line{MessageID: "m1", TS: sent, Sender: "Sharif", Text: "زي ما اتفقنا بكرة ٥ العصر"}
	same, _ := json.Marshal(MeetingTimeOptions("بكرة", "٥ العصر"))
	current := db.Meeting{TimeOptions: string(same)}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "بكرة", TimeText: "٥ العصر"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.TimeOptions != nil {
		t.Errorf("time_options should not be set when it already matches, got %q", *p.TimeOptions)
	}
}

// TestRefreshChatMeetingsEndToEnd walks one chat through a whole refresh: a
// meeting, a later cancellation message, the fake model reporting it, code
// checking the quote and writing the change.
func TestRefreshChatMeetingsEndToEnd(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس الساعة ٥", base)

	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingConfirmed, StartsAt: base + 3*24*3600,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}

	laterTS := base + 3600
	seedUpdateChatMessage(t, st, chat, "cancel", "للأسف الغينا الاجتماع خلاص", laterTS)

	updater := &fakeUpdater{updates: []ProposedUpdate{{
		Ref: 1, Status: "cancelled", EvidenceID: "cancel",
		Evidence: "للأسف الغينا الاجتماع خلاص", Note: "الاجتماع اتلغى", Confidence: 0.9,
	}}}

	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.Applied != 1 || res.Changes != 1 {
		t.Fatalf("expected one applied change, got applied=%d changes=%d rejections=%v",
			res.Applied, res.Changes, res.Rejections)
	}
	if updater.calls == 0 {
		t.Fatal("the updater should have been called at least once")
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingCancelled {
		t.Fatalf("meeting status = %q, want cancelled", got.Status)
	}
	if got.CheckedTS < laterTS {
		t.Fatalf("checked_ts = %d, should have advanced to at least %d", got.CheckedTS, laterTS)
	}
	if len(got.Changes) != 1 || got.Changes[0].Field != "status" {
		t.Fatalf("expected one status change row, got %+v", got.Changes)
	}

	// Second run: the meeting is cancelled, so it no longer needs a re-read,
	// and the model must not be called again.
	updater2 := &fakeUpdater{}
	res2, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater2, Loc: riyadh}, chat, "run-2", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings (second run): %v", err)
	}
	if updater2.calls != 0 {
		t.Errorf("second run should make zero model calls, made %d", updater2.calls)
	}
	if res2.Meetings != 0 {
		t.Errorf("second run should find nothing due a re-read, got %d", res2.Meetings)
	}
}

// TestRefreshChatMeetingsIgnoresProofTheMeetingAlreadyKnew is from the live
// run: a meeting was "resolved" with the voice note that CREATED it as proof.
// The read window starts at the last known message, so that message is in the
// first chunk. A change proved only by it is not news and must not be applied.
func TestRefreshChatMeetingsIgnoresProofTheMeetingAlreadyKnew(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "9055@s.whatsapp.net"
	base := time.Date(2026, 9, 16, 10, 11, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Karim"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "احترت اروح الجلسة التعريفية ولا لا قلت آخد رأيك", base)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "Discussion on joining the community", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "later", "صحيح صار اشي مع علي؟", base+1800)

	updater := &fakeUpdater{updates: []ProposedUpdate{{
		Ref: 1, Status: "resolved", EvidenceID: "origin",
		Evidence: "احترت اروح الجلسة التعريفية ولا لا قلت آخد رأيك",
		Note:     "They talked about joining.", Confidence: 0.9,
	}}}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.Applied != 0 {
		t.Fatalf("proof the meeting already knew must not be applied, got applied=%d", res.Applied)
	}
	got, _ := st.GetMeeting(m.ID)
	if got.Status != db.MeetingProposed {
		t.Fatalf("status = %q, want proposed", got.Status)
	}
}

// TestRefreshChatMeetingsFailedCallDoesNotAdvanceWatermark covers the other
// half of the contract: a run that could not reach the model must not mark
// the chat as read, or the change would be silently skipped forever.
func TestRefreshChatMeetingsFailedCallDoesNotAdvanceWatermark(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)

	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "later", "شو اخبار الاجتماع", base+3600)

	updater := &fakeUpdater{err: errors.New("model unreachable")}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.FailedChunks == 0 {
		t.Fatal("expected at least one failed chunk")
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.CheckedTS != 0 {
		t.Errorf("checked_ts should not advance on a failed call, got %d", got.CheckedTS)
	}
	if got.Status != db.MeetingProposed {
		t.Errorf("a failed call must not change the meeting, got status %q", got.Status)
	}
}

// TestRefreshChatMeetingsStalledMeetingKeepsWatermarkBehind covers round 2
// part A1: when a meeting's call fails on the FIRST of two chunks, a later
// chunk's success must not push checked_ts past the failed one —
// SetMeetingChecked is a MAX(), so without the "stalled" guard the second
// chunk's success alone would wrongly move it forward.
func TestRefreshChatMeetingsStalledMeetingKeepsWatermarkBehind(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)

	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}

	// 26 short filler messages force Split to produce two chunks (the
	// default MaxMessages is 25) — the first chunk's call will fail, the
	// second chunk's real evidence would otherwise wrongly get read.
	for i := 0; i < 26; i++ {
		seedUpdateChatMessage(t, st, chat, fmt.Sprintf("filler%d", i), "تمام", base+int64(i+1)*60)
	}
	cancelTS := base + 26*60 + 3600
	seedUpdateChatMessage(t, st, chat, "cancel", "للأسف الغينا الاجتماع خلاص", cancelTS)

	updater := &fakeUpdater{
		updates: []ProposedUpdate{{
			Ref: 1, Status: "cancelled", EvidenceID: "cancel",
			Evidence: "للأسف الغينا الاجتماع خلاص", Note: "الاجتماع اتلغى", Confidence: 0.9,
		}},
		failOnCall: 1,
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.Chunks < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", res.Chunks)
	}
	if res.FailedChunks == 0 {
		t.Fatal("expected the first chunk's call to fail")
	}
	// The meeting was stalled by the first chunk's failure, so it must never
	// reach a second call in this run.
	if updater.calls != 1 {
		t.Fatalf("a stalled meeting must not be asked about again this run, got %d calls", updater.calls)
	}
	if res.Changes != 0 {
		t.Fatalf("the cancellation must not apply while the meeting is stalled, got %d changes", res.Changes)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.CheckedTS != 0 {
		t.Fatalf("checked_ts must not move past the failed first chunk, got %d", got.CheckedTS)
	}
	if got.Status != db.MeetingProposed {
		t.Fatalf("a stalled meeting must not change, got status %q", got.Status)
	}
}

// TestRefreshChatMeetingsRefusesAnExcludedChat covers round 2 part A4: an
// archived (or hidden/deleted) chat must never reach the model, even when
// asked for directly by chat_jid — the same guard RunMeetings already has.
func TestRefreshChatMeetingsRefusesAnExcludedChat(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team", IsArchived: true}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	if _, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	updater := &fakeUpdater{}
	_, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err == nil {
		t.Fatal("expected an error for an archived chat")
	}
	if updater.calls != 0 {
		t.Errorf("an excluded chat must never reach the model, got %d calls", updater.calls)
	}
}

// fakeJudgeUpdater adds a second opinion on top of a fakeUpdater. Every
// earlier test uses a plain *fakeUpdater, which does NOT implement
// MeetingUpdateJudge, so RefreshChatMeetings skips the judge for them exactly
// as it would for a real engine with no second opinion — this type is only
// for the round 3 tests that need one.
type fakeJudgeUpdater struct {
	*fakeUpdater
	agrees     bool
	err        error
	judgeCalls int
}

func (f *fakeJudgeUpdater) JudgeUpdate(_ context.Context, _ UpdateClaim) (UpdateVerdict, error) {
	f.judgeCalls++
	if f.err != nil {
		return UpdateVerdict{}, f.err
	}
	return UpdateVerdict{Agrees: f.agrees, Reason: "test verdict"}, nil
}

// TestRefreshChatMeetingsSecondOpinionSaysNo covers round 3 part 3: the judge
// disagreeing drops the change, counts a rejection, but still advances the
// watermark — the model DID read the message, it just said the claim is
// wrong, which is a normal outcome and not a failure.
func TestRefreshChatMeetingsSecondOpinionSaysNo(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	laterTS := base + 3600
	seedUpdateChatMessage(t, st, chat, "cancel", "للأسف الغينا الاجتماع خلاص", laterTS)

	updater := &fakeJudgeUpdater{
		fakeUpdater: &fakeUpdater{updates: []ProposedUpdate{{
			Ref: 1, Status: "cancelled", EvidenceID: "cancel",
			Evidence: "للأسف الغينا الاجتماع خلاص", Note: "الاجتماع اتلغى", Confidence: 0.9,
		}}},
		agrees: false,
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if updater.judgeCalls != 1 {
		t.Fatalf("expected exactly one judge call, got %d", updater.judgeCalls)
	}
	if res.Judged != 1 {
		t.Errorf("Judged = %d, want 1", res.Judged)
	}
	if res.Rejections[URejectSecondOpinion] != 1 {
		t.Errorf("expected one second-opinion rejection, got %v", res.Rejections)
	}
	if res.Changes != 0 || res.Applied != 0 {
		t.Errorf("nothing should have been applied, got applied=%d changes=%d", res.Applied, res.Changes)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingProposed {
		t.Fatalf("a rejected claim must not change the meeting, got %q", got.Status)
	}
	if got.CheckedTS < laterTS {
		t.Fatalf("the model did read this message, so checked_ts should still advance, got %d", got.CheckedTS)
	}
}

// TestRefreshChatMeetingsSecondOpinionSaysNoOnAMixedUpdate covers the "drop
// only the status and timing parts" rule: a real agenda point survives even
// when the judge rejects the status claim riding along with it.
func TestRefreshChatMeetingsSecondOpinionSaysNoOnAMixedUpdate(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	laterTS := base + 3600
	evidence := "خلص الاجتماع الغينا، بس خلينا نحكي فيها لما نجتمع عن الميزانية"
	seedUpdateChatMessage(t, st, chat, "mixed", evidence, laterTS)

	updater := &fakeJudgeUpdater{
		fakeUpdater: &fakeUpdater{updates: []ProposedUpdate{{
			Ref: 1, Status: "cancelled", EvidenceID: "mixed", Evidence: evidence,
			Note: "الاجتماع اتلغى", AgendaAdd: []string{"نراجع الميزانية"}, Confidence: 0.9,
		}}},
		agrees: false,
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.Rejections[URejectSecondOpinion] != 1 {
		t.Errorf("expected one second-opinion rejection, got %v", res.Rejections)
	}
	if res.Applied != 1 || res.Changes != 1 {
		t.Fatalf("the agenda point should still apply, got applied=%d changes=%d", res.Applied, res.Changes)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingProposed {
		t.Fatalf("the rejected status claim must not apply, got %q", got.Status)
	}
	found := false
	for _, it := range got.Items {
		if it.Kind == db.ItemAgenda && it.Text == "نراجع الميزانية" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the agenda point should have survived, got items %+v", got.Items)
	}
}

// TestRefreshChatMeetingsSecondOpinionErrorStalls covers the judge-error
// case: treated exactly like a failed updater call for that one meeting —
// stalled, not applied, watermark not advanced.
func TestRefreshChatMeetingsSecondOpinionErrorStalls(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	laterTS := base + 3600
	seedUpdateChatMessage(t, st, chat, "cancel", "للأسف الغينا الاجتماع خلاص", laterTS)

	updater := &fakeJudgeUpdater{
		fakeUpdater: &fakeUpdater{updates: []ProposedUpdate{{
			Ref: 1, Status: "cancelled", EvidenceID: "cancel",
			Evidence: "للأسف الغينا الاجتماع خلاص", Note: "الاجتماع اتلغى", Confidence: 0.9,
		}}},
		err: errors.New("second opinion unreachable"),
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if res.FailedChunks == 0 {
		t.Fatal("expected the failed judge call to count as a failed chunk")
	}
	if res.Changes != 0 {
		t.Errorf("nothing should apply when the judge could not be reached, got %d changes", res.Changes)
	}

	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingProposed {
		t.Fatalf("a stalled meeting must not change, got %q", got.Status)
	}
	if got.CheckedTS != 0 {
		t.Fatalf("a failed judge call must not advance the watermark, got %d", got.CheckedTS)
	}
}

// TestRefreshChatMeetingsAgendaOnlyUpdateNeverCallsTheJudge covers
// updateNeedsVerdict: agenda, mode, location and link are the chat's own
// words, not a judgement call, so they never need a second opinion.
func TestRefreshChatMeetingsAgendaOnlyUpdateNeverCallsTheJudge(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	if _, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	}); err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	laterTS := base + 3600
	evidence := "خلينا نحكي فيها لما نجتمع عن الميزانية"
	seedUpdateChatMessage(t, st, chat, "agenda", evidence, laterTS)

	updater := &fakeJudgeUpdater{
		fakeUpdater: &fakeUpdater{updates: []ProposedUpdate{{
			Ref: 1, EvidenceID: "agenda", Evidence: evidence,
			AgendaAdd: []string{"نراجع الميزانية"}, Confidence: 0.9,
		}}},
		agrees: false, // if the judge WERE called, this would drop everything
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if updater.judgeCalls != 0 {
		t.Fatalf("an agenda-only update must never reach the judge, got %d calls", updater.judgeCalls)
	}
	if res.Judged != 0 {
		t.Errorf("Judged = %d, want 0", res.Judged)
	}
	if res.Applied != 1 || res.Changes != 1 {
		t.Fatalf("the agenda point should apply on its own, got applied=%d changes=%d", res.Applied, res.Changes)
	}
}

// TestBuildPatchVagueTimeDoesNotReplaceAnExactOne covers round 3 part 4: a
// day word with no clock time and no part of day, landing on the SAME day
// the meeting is already set for, must not push an exact, already-agreed
// time down to the resolver's neutral noon.
func TestBuildPatchVagueTimeDoesNotReplaceAnExactOne(t *testing.T) {
	sameDay := time.Date(2026, 9, 17, 17, 0, 0, 0, riyadh).Unix() // 5pm, exact
	current := db.Meeting{StartsAt: sameDay, TimeOptions: `["الخميس","٥ العصر"]`}

	// "اليوم" said earlier the same day: no clock time, no part-of-day word.
	morning := time.Date(2026, 9, 17, 9, 0, 0, 0, riyadh).Unix()
	line := Line{MessageID: "m1", TS: morning, Sender: "Sharif", Text: "تمام، اليوم"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "اليوم"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt != nil {
		t.Fatalf("a vague 'still today' should not replace the exact 5pm, got %v", *p.StartsAt)
	}
	if p.TimeOptions != nil {
		t.Fatalf("time_options should also stay untouched, got %q", *p.TimeOptions)
	}
}

// TestBuildPatchExactTimeOnTheSameDayStillApplies is the companion case: a
// real clock time, even on the same day the meeting is already set for, must
// still move it.
func TestBuildPatchExactTimeOnTheSameDayStillApplies(t *testing.T) {
	sameDay := time.Date(2026, 9, 17, 17, 0, 0, 0, riyadh).Unix()
	current := db.Meeting{StartsAt: sameDay}

	morning := time.Date(2026, 9, 17, 9, 0, 0, 0, riyadh).Unix()
	line := Line{MessageID: "m1", TS: morning, Sender: "Sharif", Text: "خليها اليوم ٦ المسا"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{WhenText: "اليوم", TimeText: "٦ المسا"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, current, riyadh, "run-1", "chat@g.us")
	if p.StartsAt == nil {
		t.Fatal("an exact new time should still move the meeting, even on the same day")
	}
}

// TestVerifyUpdateAgendaMustBeGrounded covers round 4 part 1. Live bug: the
// model saved the PROMPT'S OWN example phrases as agenda lines, twelve
// times. None of those words are ever said in a real chat, so nothing
// survives, and with nothing else in the update it is rejected as empty.
func TestVerifyUpdateAgendaMustBeGrounded(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	evidence := "شكرا للجميع، بنتكلم لاحقا"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9,
		AgendaAdd: []string{
			"نناقش في الاجتماع كذا", "ضيف على الأجندة", "خلينا نحكي فيها لما نجتمع",
		},
	}
	if _, reason, ok := VerifyUpdate(chunk, known, u); ok || reason != URejectEmpty {
		t.Fatalf("ungrounded prompt-example agenda lines should leave an empty update, got ok=%v reason=%q", ok, reason)
	}
}

// TestVerifyUpdateLinkMustBeInEvidence and TestVerifyUpdateLinkKeptWhenCodeIsInEvidence
// cover round 4 part 2a: a link claim survives only when its join code is
// really quoted in the evidence message.
func TestVerifyUpdateLinkMustBeInEvidence(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	evidence := "خلص حاضر، خلينا نحكي فيها لما نجتمع عن الميزانية"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{
		Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9,
		Link:      "https://meet.google.com/gtm-icgr-twt", // not said anywhere in this message
		AgendaAdd: []string{"نراجع الميزانية"},
	}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	if !ok {
		t.Fatalf("dropped as %s", reason)
	}
	if v.Update.Link != "" {
		t.Errorf("a link with no code in the evidence should be cleared, got %q", v.Update.Link)
	}
}

func TestVerifyUpdateLinkKeptWhenCodeIsInEvidence(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	link := "https://meet.google.com/gtm-icgr-twt"
	evidence := "بديل رابط الاجتماع، هذا هو الرابط: " + link
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}
	u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9, Link: link}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	if !ok {
		t.Fatalf("dropped as %s", reason)
	}
	if v.Update.Link != link {
		t.Errorf("a link whose code is really in the evidence should survive, got %q", v.Update.Link)
	}
}

// TestVerifyUpdateBareLinkTiesOnlyNearAKnownTime covers round 4 part 2b.
// Live bug: three IN-PERSON meetings in another city each got a Google Meet link
// because a bare link message, days away from the meeting, got tied to
// whichever meeting was open. A bare link belongs to a meeting only when
// that meeting is dated and the link arrived close to its own time.
func TestVerifyUpdateBareLinkTiesOnlyNearAKnownTime(t *testing.T) {
	link := "https://meet.google.com/gtm-icgr-twt?authuser=0"

	t.Run("undated meeting: cleared", func(t *testing.T) {
		now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: link}}}
		known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع جدة", Status: db.MeetingProposed}}
		u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: link, Confidence: 0.9, Link: link}
		v, _, _ := VerifyUpdate(chunk, known, u)
		if v.Update.Link != "" {
			t.Errorf("a bare link for an undated meeting should be cleared, got %q", v.Update.Link)
		}
	})

	t.Run("days before a dated meeting: cleared", func(t *testing.T) {
		start := time.Date(2026, 9, 20, 17, 0, 0, 0, riyadh).Unix()
		sent := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix() // 8 days before
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: sent, Sender: "Sharif", Text: link}}}
		known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع جدة", Status: db.MeetingConfirmed, StartsAt: start}}
		u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: link, Confidence: 0.9, Link: link}
		v, _, _ := VerifyUpdate(chunk, known, u)
		if v.Update.Link != "" {
			t.Errorf("a bare link days before a dated meeting should be cleared, got %q", v.Update.Link)
		}
	})

	t.Run("10 minutes before a dated meeting: kept", func(t *testing.T) {
		start := time.Date(2026, 9, 20, 17, 0, 0, 0, riyadh).Unix()
		sent := start - 10*60
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: sent, Sender: "Sharif", Text: link}}}
		known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع جدة", Status: db.MeetingConfirmed, StartsAt: start}}
		u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: link, Confidence: 0.9, Link: link}
		v, reason, ok := VerifyUpdate(chunk, known, u)
		if !ok {
			t.Fatalf("dropped as %s", reason)
		}
		if v.Update.Link != link {
			t.Errorf("a bare link posted 10 minutes before a dated meeting should be kept, got %q", v.Update.Link)
		}
	})
}

// TestUpdateNeedsVerdictCoversLink and TestClaimForUpdateIncludesTheLink cover
// round 4 part 2c: a link change now needs a second opinion, same as a
// status or timing change.
func TestUpdateNeedsVerdictCoversLink(t *testing.T) {
	if !updateNeedsVerdict(ProposedUpdate{Link: "https://meet.google.com/gtm-icgr-twt"}) {
		t.Error("a link change should need a second opinion")
	}
}

func TestClaimForUpdateIncludesTheLink(t *testing.T) {
	claim := claimForUpdate(ProposedUpdate{Link: "https://meet.google.com/gtm-icgr-twt"})
	want := "The join link for this meeting is: https://meet.google.com/gtm-icgr-twt."
	if claim != want {
		t.Errorf("claim = %q, want %q", claim, want)
	}
}

// TestRefreshChatMeetingsSecondOpinionClearsARejectedLink is the end-to-end
// half of round 4 part 2c: a link claim goes through the judge, and a
// rejected link never gets applied, same as any other rejected part.
func TestRefreshChatMeetingsSecondOpinionClearsARejectedLink(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, riyadh).Unix()

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	seedUpdateChatMessage(t, st, chat, "origin", "نرتب اجتماع الخميس", base)
	m, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingProposed,
		OriginChatJID: chat, OriginMessageID: "origin",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	if err := st.LinkMeetingMessage(m.ID, chat, "origin", "scheduling"); err != nil {
		t.Fatalf("LinkMeetingMessage: %v", err)
	}
	laterTS := base + 3600
	link := "https://meet.google.com/gtm-icgr-twt"
	evidence := "بديل رابط الاجتماع، هذا هو الرابط: " + link
	seedUpdateChatMessage(t, st, chat, "link", evidence, laterTS)

	updater := &fakeJudgeUpdater{
		fakeUpdater: &fakeUpdater{updates: []ProposedUpdate{{
			Ref: 1, EvidenceID: "link", Evidence: evidence, Link: link, Confidence: 0.9,
		}}},
		agrees: false,
	}
	res, err := RefreshChatMeetings(context.Background(),
		UpdateDeps{Store: st, Updater: updater, Loc: riyadh}, chat, "run-1", nil)
	if err != nil {
		t.Fatalf("RefreshChatMeetings: %v", err)
	}
	if updater.judgeCalls != 1 {
		t.Fatalf("a link claim should reach the judge, got %d calls", updater.judgeCalls)
	}
	if res.Rejections[URejectSecondOpinion] != 1 {
		t.Errorf("expected one second-opinion rejection, got %v", res.Rejections)
	}
	got, err := st.GetMeeting(m.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Link != "" {
		t.Fatalf("a rejected link claim must not apply, got %q", got.Link)
	}
}

// TestVerifyUpdateModeMustBeSaidInEvidence covers round 4 part 2d: a mode
// claim survives only when the evidence message itself names it.
func TestVerifyUpdateModeMustBeSaidInEvidence(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingProposed}}

	t.Run("not said: cleared", func(t *testing.T) {
		evidence := "خلص حاضر، خلينا نحكي فيها لما نجتمع عن الميزانية"
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
		u := ProposedUpdate{
			Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9,
			Mode: "online", AgendaAdd: []string{"نراجع الميزانية"},
		}
		v, reason, ok := VerifyUpdate(chunk, known, u)
		if !ok {
			t.Fatalf("dropped as %s", reason)
		}
		if v.Update.Mode != "" {
			t.Errorf("a mode the evidence never names should be cleared, got %q", v.Update.Mode)
		}
	})

	t.Run("said online: kept", func(t *testing.T) {
		evidence := "خلونا نسويها اونلاين عن طريق زوم"
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
		u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9, Mode: "online"}
		v, reason, ok := VerifyUpdate(chunk, known, u)
		if !ok {
			t.Fatalf("dropped as %s", reason)
		}
		if v.Update.Mode != "online" {
			t.Errorf("a mode the evidence names should survive, got %q", v.Update.Mode)
		}
	})

	t.Run("said in person: kept", func(t *testing.T) {
		evidence := "الاجتماع حضوري بمكتبنا"
		chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: now, Sender: "Sharif", Text: evidence}}}
		u := ProposedUpdate{Ref: 1, EvidenceID: "m1", Evidence: evidence, Confidence: 0.9, Mode: "in_person"}
		v, reason, ok := VerifyUpdate(chunk, known, u)
		if !ok {
			t.Fatalf("dropped as %s", reason)
		}
		if v.Update.Mode != "in_person" {
			t.Errorf("a mode the evidence names should survive, got %q", v.Update.Mode)
		}
	})
}

// TestBuildPatchLinkMakesAnUndatedModeMeetingOnline and
// TestBuildPatchLinkNeverOverridesAnExistingMode cover the rest of round 4
// part 2d: a link alone never changes a mode that is already set, but a
// meeting with no mode yet, given an accepted link, may become online.
func TestBuildPatchLinkMakesAnUndatedModeMeetingOnline(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{Link: "https://meet.google.com/gtm-icgr-twt"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, db.Meeting{Mode: ""}, riyadh, "run-1", "chat@g.us")
	if p.Mode == nil || *p.Mode != "online" {
		t.Fatalf("a link on a meeting with no mode yet should make it online, got %v", p.Mode)
	}
}

func TestBuildPatchLinkNeverOverridesAnExistingMode(t *testing.T) {
	line := Line{MessageID: "m1", TS: time.Now().Unix(), Sender: "Sharif"}
	v := VerifiedUpdate{
		Update: ProposedUpdate{Link: "https://meet.google.com/gtm-icgr-twt"},
		Line:   line,
		Chunk:  Chunk{Lines: []Line{line}},
	}
	p := BuildPatch(v, db.Meeting{Mode: "in_person"}, riyadh, "run-1", "chat@g.us")
	if p.Mode != nil {
		t.Fatalf("a link alone must not change an already-set mode, got %v", *p.Mode)
	}
}

// TestVerifyUpdatePastMeetingDoesNotMove covers round 4 part 3a. Live bug:
// "نجلس الأحد قبل اجتماع orbit نشوف قصة الQRT" moved the dates of TWO
// meetings whose own dates had already passed — it was about a THIRD
// meeting. Talk more than 24h after a meeting's own start does not move it.
func TestVerifyUpdatePastMeetingDoesNotMove(t *testing.T) {
	start := time.Date(2026, 9, 1, 17, 0, 0, 0, riyadh).Unix()
	sent := start + 25*3600 // more than 24h after the meeting's own start
	known := []KnownMeeting{{
		Ref: 1, ID: 42, Title: "Al-Najjar – ERP agents partnership session",
		Status: db.MeetingConfirmed, StartsAt: start,
	}}
	evidence := "نجلس الأحد قبل اجتماع orbit نشوف قصة الQRT"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: sent, Sender: "Sharif", Text: evidence}}}
	u := ProposedUpdate{Ref: 1, WhenText: "الأحد", EvidenceID: "m1", Evidence: evidence, Confidence: 0.9}
	v, reason, ok := VerifyUpdate(chunk, known, u)
	// The timing was dropped, and with nothing else in the update it is
	// rejected as empty rather than silently moving a meeting that already
	// happened.
	if ok || reason != URejectEmpty {
		t.Fatalf("timing on an already-passed meeting should leave nothing, got ok=%v reason=%q", ok, reason)
	}
	if v.Update.WhenText != "" {
		t.Errorf("when_text should have been cleared, got %q", v.Update.WhenText)
	}
}

// TestVerifyUpdateHeldLongAfterStartStillPasses is the accept half of round 4
// part 3a: a closing status (held/cancelled/resolved) is not timing, so it
// still passes long after the meeting's own start.
func TestVerifyUpdateHeldLongAfterStartStillPasses(t *testing.T) {
	start := time.Date(2026, 9, 1, 17, 0, 0, 0, riyadh).Unix()
	sent := start + 25*3600
	known := []KnownMeeting{{Ref: 1, ID: 42, Title: "اجتماع الفريق", Status: db.MeetingConfirmed, StartsAt: start}}
	evidence := "الاجتماع كان تمام، شكرا للجميع"
	chunk := Chunk{Lines: []Line{{MessageID: "m1", TS: sent, Sender: "Sharif", Text: evidence}}}
	u := ProposedUpdate{Ref: 1, Status: "held", EvidenceID: "m1", Evidence: evidence, Confidence: 0.9}
	if _, reason, ok := VerifyUpdate(chunk, known, u); !ok {
		t.Fatalf("a held status long after the meeting's start should still pass, dropped as %s", reason)
	}
}

// fakeFinder is a MeetingFinder that answers with fixed proposals, for the
// revive test below — the same trick fakeUpdater plays for the updater side.
type fakeFinder struct{ meetings []ProposedMeeting }

func (f *fakeFinder) Name() string { return "fake:finder-test" }

func (f *fakeFinder) FindMeetings(_ context.Context, _ MeetingInput) (MeetingOutput, error) {
	return MeetingOutput{Meetings: f.meetings}, nil
}

// TestRunMeetingsRevivesALapsedMeeting covers step 6 of the brief: when the
// finder attaches a new scheduling message to a meeting that had already gone
// lapsed (see CloseStaleMeetings), it must come back to "proposed" rather
// than stay dead. The join link is what ties the new message to the old
// meeting here, the same way findExistingMeeting always matches a link.
func TestRunMeetingsRevivesALapsedMeeting(t *testing.T) {
	st := newUpdateTestStore(t)
	chat := "team@g.us"

	if err := st.StoreChat(&db.Chat{JID: chat, Name: "Delivery team"}); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}

	link := "https://meet.google.com/abc-defg-hij"
	lapsed, err := st.CreateMeeting(&db.Meeting{
		Title: "اجتماع الفريق", Status: db.MeetingLapsed, Link: link,
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	text := "خلونا نجتمع بكرة على هذا الرابط " + link
	ts := time.Now().Add(-1 * time.Hour).Unix()
	seedUpdateChatMessage(t, st, chat, "revive", text, ts)

	finder := &fakeFinder{meetings: []ProposedMeeting{{
		Title: "اجتماع الفريق", EvidenceID: "revive", Evidence: text,
		WhenText: "بكرة", Status: "proposed", Link: link, Confidence: 0.9,
	}}}

	res, err := RunMeetings(context.Background(),
		MeetingDeps{Store: st, Finder: finder, Loc: riyadh},
		RunSpec{ChatJID: chat, RunID: "run-1"}, nil)
	if err != nil {
		t.Fatalf("RunMeetings: %v", err)
	}
	if res.Attached != 1 {
		t.Fatalf("expected the message to attach to the existing meeting, got %+v", res)
	}

	got, err := st.GetMeeting(lapsed.ID)
	if err != nil {
		t.Fatalf("GetMeeting: %v", err)
	}
	if got.Status != db.MeetingProposed {
		t.Fatalf("a lapsed meeting that came up again should be proposed, got %q", got.Status)
	}
	var revived bool
	for _, c := range got.Changes {
		if c.Field == "status" && c.NewValue == db.MeetingProposed && c.Source == db.ChangeByModel {
			revived = true
		}
	}
	if !revived {
		t.Fatalf("expected a model-sourced status change to proposed, got %+v", got.Changes)
	}
}

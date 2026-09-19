package extract

import (
	"testing"
	"time"
)

// Every case here is a real message, or the shape of one. The point of the
// checks in meetings_verify.go is that a model may be wrong; these tests are
// what say so out loud.

var riyadh = mustLoad("Asia/Riyadh")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// TestStartsNow covers round 4 part 4: a duration of minutes or part of an
// hour coming up, and "as soon as you're ready", both spot a call starting
// now. Real strings from one live chat: "انا ممكن اكون متاح خلال ١٠ دقايق
// ربع ساعة اذا بيناسبك" / "تمام وقت ما تكون جاهز ابعتلي".
func TestStartsNow(t *testing.T) {
	yes := []string{
		"انا ممكن اكون متاح خلال ١٠ دقايق ربع ساعة اذا بيناسبك",
		"تمام وقت ما تكون جاهز ابعتلي",
		"اول ما تجهز قلي",
		"يلا بانتظارك على الاجتماع",
		"اتصل فيني الحين",
		"خلص هلق دخلت",
		"in 10 minutes",
		"in half an hour",
		"call me right now",
	}
	for _, s := range yes {
		if !startsNow(s) {
			t.Errorf("should read as a call starting now: %q", s)
		}
	}

	no := []string{
		"نجتمع الخميس الساعة ٥",
		"خلص حاضر الليلة بنحكي",
		"بعد اسبوعين ان شاء الله",
		"",
	}
	for _, s := range no {
		if startsNow(s) {
			t.Errorf("should NOT read as a call starting now: %q", s)
		}
	}
}

func TestMentionsMeetingTalk(t *testing.T) {
	yes := []string{
		"كيف مواعيدكم نرتب اجتماع",
		"نجتمع الخميس الساعة ٥",
		"خلينا نتقابل ونحكي في الموضوع",
		"لازم نعمل ميتنج قريب",
		"let's meet on Tuesday",
		"https://meet.google.com/abc-defg-hij",
		"Hassan has invited you to join a video meeting on Google Meet",
		"الجلسة القادمة يوم الأحد",
		// A call set up with a talk verb and a time, and no meeting word.
		"خلينا نتكلم اليوم بعد العصر، ونتفاهم على التفاصيل",
		"خليني احكي معك على رواق المسا إذا مناسب إلك",
		"بس ضروري نحكي اليوم ما بدي اسيب الموضوع",
		"خلص حاضر الليلة بنحكي",
		"let's talk tomorrow morning",
	}
	for _, s := range yes {
		if !MentionsMeetingTalk(s) {
			t.Errorf("should read as meeting talk: %q", s)
		}
	}

	no := []string{
		"مرحبا شباب",
		"اخباركم",
		"جهز العقد قبل بكرة",
		"التقرير بيطلع فاضي",
		"thanks, got it",
		"",
		// A talk verb with no time, and a talk that already happened.
		"بدي احكي معك بموضوع",
		"حكيت مع علي اليوم وكل شي تمام",
	}
	for _, s := range no {
		if MentionsMeetingTalk(s) {
			t.Errorf("should NOT read as meeting talk: %q", s)
		}
	}
}

// The bug this whole anchor exists for. A group said "بلكي بكرة" on the 7th,
// went quiet, and reopened on the 12th. The date must not become the 13th.
func TestStaleTimingWordsGetNoDate(t *testing.T) {
	sep7 := time.Date(2026, 9, 7, 15, 6, 0, 0, riyadh).Unix()
	sep12 := time.Date(2026, 9, 12, 15, 21, 0, 0, riyadh).Unix()

	chunk := Chunk{Lines: []Line{
		{MessageID: "old", TS: sep7, Sender: "Hassan", Text: "بلكي بكرة ان شاء الله"},
		{MessageID: "new", TS: sep12, Sender: "Sharif", Text: "كيف مواعيدكم نرتب اجتماع"},
	}}
	v := VerifiedMeeting{
		Meeting: ProposedMeeting{Title: "اجتماع", WhenText: "بكرة"},
		Line:    chunk.Lines[1],
	}
	if got := ResolveMeetingStartInChunk(chunk, v, riyadh); got != 0 {
		t.Fatalf("stale 'بكرة' should give no date, got %s",
			time.Unix(got, 0).In(riyadh).Format(time.RFC3339))
	}
}

// Availability normally arrives as a REPLY, so the timing words sit after the
// message that proves the meeting. That must still date it.
func TestTimingWordsInALaterReply(t *testing.T) {
	base := time.Date(2026, 9, 12, 15, 21, 0, 0, riyadh)
	chunk := Chunk{Lines: []Line{
		{MessageID: "ask", TS: base.Unix(), Sender: "Sharif", Text: "كيف مواعيدكم نرتب اجتماع"},
		{MessageID: "reply", TS: base.Add(time.Minute).Unix(), Sender: "Mohammed",
			Text: "انا اليوم متاح بالكامل بأي وقت"},
	}}
	v := VerifiedMeeting{
		Meeting: ProposedMeeting{Title: "اجتماع", WhenText: "اليوم"},
		Line:    chunk.Lines[0],
	}
	got := ResolveMeetingStartInChunk(chunk, v, riyadh)
	if got == 0 {
		t.Fatal("'اليوم' said one minute later should still date the meeting")
	}
	if d := time.Unix(got, 0).In(riyadh); d.Day() != 12 || d.Month() != time.September {
		t.Fatalf("wrong day: %s", d.Format(time.RFC3339))
	}
}

// A neutral noon stands in for an unnamed hour, and it must not make a meeting
// proposed at half past three look like it already happened.
func TestTodayAfterNoonIsNotPast(t *testing.T) {
	afternoon := time.Date(2026, 9, 12, 15, 30, 0, 0, riyadh).Unix()
	chunk := Chunk{Lines: []Line{
		{MessageID: "a", TS: afternoon, Sender: "Sharif", Text: "نجتمع اليوم؟"},
	}}
	v := VerifiedMeeting{
		Meeting: ProposedMeeting{Title: "اجتماع", WhenText: "اليوم"},
		Line:    chunk.Lines[0],
	}
	if ResolveMeetingStartInChunk(chunk, v, riyadh) == 0 {
		t.Fatal("a meeting proposed for today must keep today's date")
	}
}

func TestResolveMeetingStartClockTimes(t *testing.T) {
	// A Saturday, so weekday arithmetic is easy to read.
	sent := time.Date(2026, 9, 12, 10, 0, 0, 0, riyadh).Unix()

	cases := []struct {
		when, clock string
		wantHour    int
		wantDay     int
	}{
		{"بكرة", "٥ العصر", 17, 13},
		{"بكرة", "الساعة ٤", 16, 13},
		{"اليوم", "2pm", 14, 12},
		{"اليوم", "14:30", 14, 12},
		{"اليوم", "9 صباحا", 9, 12},
		{"بعد بكرة", "", 12, 14},
		// No day named at all: real words, but not a date.
		{"قريب", "5pm", 0, 0},
		{"", "5pm", 0, 0},
	}
	for _, c := range cases {
		got := ResolveMeetingStart(c.when, c.clock, sent, riyadh)
		if c.wantDay == 0 {
			if got != 0 {
				t.Errorf("%q %q: expected no date, got %s", c.when, c.clock,
					time.Unix(got, 0).In(riyadh))
			}
			continue
		}
		d := time.Unix(got, 0).In(riyadh)
		if d.Day() != c.wantDay || d.Hour() != c.wantHour {
			t.Errorf("%q %q: got %s, wanted day %d hour %d",
				c.when, c.clock, d.Format("Jan 2 15:04"), c.wantDay, c.wantHour)
		}
	}
}

// TestResolveMeetingStartPartOfDay covers round 2 part B5: a part-of-day word
// with no exact clock time must not fall back to a flat noon.
func TestResolveMeetingStartPartOfDay(t *testing.T) {
	sent := time.Date(2026, 9, 12, 10, 0, 0, 0, riyadh).Unix()

	cases := []struct {
		when, clock string
		wantHour    int
	}{
		{"الليلة", "", 21},
		{"بكرة", "بعد العشا", 21},
		{"اليوم", "مساء", 19},
		{"بكرة", "بعد المغرب", 19},
		{"اليوم", "بعد العصر", 17},
		{"بكرة", "العصر", 17},
		{"اليوم", "الظهر", 13},
		{"بكرة", "الصباح", 10},
		// A day with truly no time-of-day word at all still gets the old
		// neutral noon.
		{"بعد بكرة", "", 12},
	}
	for _, c := range cases {
		got := ResolveMeetingStart(c.when, c.clock, sent, riyadh)
		if got == 0 {
			t.Fatalf("%q %q: expected a date, got none", c.when, c.clock)
		}
		if d := time.Unix(got, 0).In(riyadh); d.Hour() != c.wantHour {
			t.Errorf("%q %q: got hour %d, wanted %d", c.when, c.clock, d.Hour(), c.wantHour)
		}
	}

	// An exact clock time still wins over a part-of-day word said alongside it.
	got := ResolveMeetingStart("الليلة", "٥ العصر", sent, riyadh)
	if d := time.Unix(got, 0).In(riyadh); d.Hour() != 17 {
		t.Errorf("an exact time should win over 'الليلة', got hour %d", d.Hour())
	}
}

func TestVerifyMeetingRejections(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	line := func(id, text string) Line {
		return Line{MessageID: id, TS: now, Sender: "Sharif", Text: text}
	}

	cases := []struct {
		name string
		line Line
		p    ProposedMeeting
		want MeetingReject
	}{
		{
			name: "quote is not in the message",
			line: line("m1", "كيف مواعيدكم نرتب اجتماع"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "نجتمع الخميس الساعة خمسة في المكتب", Confidence: 0.9},
			want: MRejectBadQuote,
		},
		{
			name: "names a message that is not here",
			line: line("m1", "نرتب اجتماع"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "nope",
				Evidence: "نرتب اجتماع", Confidence: 0.9},
			want: MRejectNoEvidence,
		},
		{
			name: "the message says nothing about meeting",
			line: line("m1", "ابعتلي الملف لو سمحت"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "ابعتلي الملف لو سمحت", Confidence: 0.9},
			want: MRejectNotMeeting,
		},
		{
			name: "their own calendar",
			line: line("m1", "عندي اجتماع الساعة ٧ ما رح اقدر ارد"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "عندي اجتماع الساعة ٧ ما رح اقدر ارد", Confidence: 0.9},
			want: MRejectTheirs,
		},
		{
			name: "a call starting now",
			line: line("m1", "يلا بانتظارك على الاجتماع"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "يلا بانتظارك على الاجتماع", Confidence: 0.9},
			want: MRejectAdHoc,
		},
		{
			name: "a bare join link",
			line: line("m1", "https://meet.google.com/abc-defg-hij"),
			p: ProposedMeeting{Title: "call", EvidenceID: "m1",
				Evidence: "https://meet.google.com/abc-defg-hij", Confidence: 0.9},
			want: MRejectAdHoc,
		},
		{
			name: "already happened",
			line: line("m1", "الاجتماع كان ممتاز شكرا للجميع"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "الاجتماع كان ممتاز شكرا للجميع", Confidence: 0.9},
			want: MRejectPast,
		},
		{
			name: "not sure enough",
			line: line("m1", "يمكن نجتمع"),
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "يمكن نجتمع", Confidence: 0.3},
			want: MRejectLowScore,
		},
		{
			name: "only proof is a carried-in line",
			line: Line{MessageID: "m1", TS: now, Sender: "Sharif",
				Text: "نرتب اجتماع الخميس", Context: true},
			p: ProposedMeeting{Title: "اجتماع", EvidenceID: "m1",
				Evidence: "نرتب اجتماع الخميس", Confidence: 0.9},
			want: MRejectContext,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, reason, ok := VerifyMeeting(Chunk{Lines: []Line{c.line}}, c.p)
			if ok {
				t.Fatalf("should have been dropped as %s, but was kept", c.want)
			}
			if reason != c.want {
				t.Fatalf("dropped as %s, wanted %s", reason, c.want)
			}
		})
	}
}

func TestVerifyMeetingKeepsRealOnes(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, riyadh).Unix()
	cases := []struct{ text, title string }{
		{"كيف مواعيدكم نرتب اجتماع", "اجتماع لترتيب البرنامج"},
		{"نجتمع الخميس الساعة ٥ في مكتبنا", "اجتماع الخميس"},
		{"خلينا نعمل اجتماع الأسبوع الجاي لمناقشة العقد", "اجتماع مناقشة العقد"},
		{"الاجتماع الحضوري في مكتبنا بجاده ٣٠، انتظر تأكيدكم", "اجتماع حضوري"},
	}
	for _, c := range cases {
		chunk := Chunk{Lines: []Line{
			{MessageID: "m1", TS: now, Sender: "Sharif", Text: c.text},
		}}
		p := ProposedMeeting{Title: c.title, EvidenceID: "m1", Evidence: c.text, Confidence: 0.8}
		if _, reason, ok := VerifyMeeting(chunk, p); !ok {
			t.Errorf("%q was dropped as %s, but it is a real meeting", c.text, reason)
		}
	}
}

func TestMeetingModeIsInferred(t *testing.T) {
	cases := []struct {
		p    ProposedMeeting
		want string
	}{
		{ProposedMeeting{Link: "https://meet.google.com/abc-defg-hij"}, "online"},
		{ProposedMeeting{Location: "مكتبنا بجاده ٣٠"}, "in_person"},
		{ProposedMeeting{Mode: "in-person"}, "in_person"},
		{ProposedMeeting{}, ""},
	}
	for _, c := range cases {
		if got := meetingMode(c.p); got != c.want {
			t.Errorf("mode %+v: got %q, wanted %q", c.p, got, c.want)
		}
	}
}

// "confirmed" with no date is a contradiction: nothing was confirmed.
func TestConfirmedNeedsADate(t *testing.T) {
	p := ProposedMeeting{Status: "confirmed"}
	if got := meetingStatus(p, 0); got != "proposed" {
		t.Errorf("confirmed with no date should fall back to proposed, got %q", got)
	}
	if got := meetingStatus(p, 1789000000); got != "confirmed" {
		t.Errorf("confirmed with a date should stay confirmed, got %q", got)
	}
}

// The bug that made the first live run write 58 meetings from one chat.
//
// The keyword pass looked back a month, but the model run used the stored
// watermark — which is 0 for a chat nobody has scanned — so it read the whole
// history and proposed every meeting since the chat began. The floor is what
// stops that, and it must NOT apply when a caller asked for a window.
func TestFirstLookFloor(t *testing.T) {
	now := time.Now().Unix()

	// No Since given: a never-scanned chat starts one week back, not at zero.
	got := firstLookFloor(0, 0, now)
	want := now - FirstLook
	if got != want {
		t.Errorf("never scanned: since %d, wanted %d (a week back)", got, want)
	}

	// A watermark inside the window is respected as-is.
	recent := now - 3600
	if got := firstLookFloor(0, recent, now); got != recent {
		t.Errorf("recent watermark should be kept, got %d", got)
	}

	// An explicit Since is a deliberate backfill and is never raised.
	old := now - 365*24*3600
	if got := firstLookFloor(old, 0, now); got != old {
		t.Errorf("an explicit Since must be honoured, got %d", got)
	}
}

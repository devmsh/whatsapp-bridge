package extract_test

import (
	"testing"
	"time"

	"whatsapp-bridge-v2/internal/extract"
)

func chunkWith(lines ...extract.Line) extract.Chunk {
	return extract.Chunk{ChatJID: "c@g.us", Lines: lines}
}

func proposal(id, quote, title string) extract.ProposedTask {
	return extract.ProposedTask{
		Title: title, EvidenceID: id, Evidence: quote,
		OwnerText: "unknown", Confidence: 0.9,
	}
}

// TestVerifyRejectsFabrication is the guarantee the whole engine rests on: a
// quote that is not in the message it names is not a weak task, it is made up.
func TestVerifyRejectsFabrication(t *testing.T) {
	c := chunkWith(line("M1", 100, "Sara", "جهز العقد قبل الخميس"))

	if _, _, ok := extract.Verify(c, proposal("#M1", "جهز العقد", "prepare the contract")); !ok {
		t.Errorf("a quote that IS in the message must pass")
	}

	bad := []struct {
		name string
		p    extract.ProposedTask
		want extract.RejectReason
	}{
		{"words never said", proposal("#M1", "ابعتلي التقرير", "send the report"), extract.RejectBadQuote},
		{"message not here", proposal("#NOPE", "جهز العقد", "x"), extract.RejectNoEvidenceID},
		{"no title", proposal("#M1", "جهز العقد", "  "), extract.RejectEmptyTitle},
	}
	for _, tc := range bad {
		_, reason, ok := extract.Verify(c, tc.p)
		if ok {
			t.Errorf("%s: should have been rejected", tc.name)
			continue
		}
		if reason != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, reason, tc.want)
		}
	}
}

// TestVerifyAcceptsContextLines — a request often only becomes visible once
// somebody answers it, and the answer lands in the next chunk, where the
// request itself is only context.
//
// This used to be a rejection. Measured against hand-labelled chats it threw
// away fourteen correct tasks in a single run, so the rule was removed and the
// duplicate it allows is handled in dedupe, where duplicates belong.
func TestVerifyAcceptsContextLines(t *testing.T) {
	ctx := line("M0", 50, "Sami", "جهز العقد قبل الخميس")
	ctx.Context = true
	c := chunkWith(ctx, line("M1", 100, "Sara", "تمام"))

	if _, reason, ok := extract.Verify(c,
		proposal("#M0", "جهز العقد قبل الخميس", "جهز العقد")); !ok {
		t.Errorf("a request in a context line is still a request (reason %q)", reason)
	}
}

// TestVerifyDropsBareQuestions is the rule the prompt could not enforce. Asked
// not to return questions, qwen2.5 still offered "ممكن مثال؟" at 0.8
// confidence, so the rule lives in code where it is enforced rather than
// requested.
func TestVerifyDropsBareQuestions(t *testing.T) {
	c := chunkWith(
		line("Q1", 100, "Sara", "ممكن مثال؟"),
		line("Q2", 101, "Sami", "شو رايكم؟"),
		line("Q3", 102, "Sara", "ممكن تبعتلي الملف؟"),
		line("Q4", 103, "Sami", "any update?"),
	)
	for _, id := range []string{"#Q1", "#Q2", "#Q4"} {
		var quote string
		for _, l := range c.Lines {
			if "#"+l.MessageID == id {
				quote = l.Text
			}
		}
		if _, reason, ok := extract.Verify(c, proposal(id, quote, "t")); ok {
			t.Errorf("%s (%q) is a question, not an assignment", id, quote)
		} else if reason != extract.RejectQuestion {
			t.Errorf("%s: reason = %q, want question_only", id, reason)
		}
	}
	// A question that carries a request is still a request.
	if _, _, ok := extract.Verify(c, proposal("#Q3", "ممكن تبعتلي الملف؟", "send the file")); !ok {
		t.Errorf("a question containing a request is a task")
	}
}

// TestVerifyIsLenientWithTranscripts — models tidy up ASR output while quoting
// it. The quote must still be anchored to a real message, so this loosens the
// match without loosening the check.
func TestVerifyIsLenientWithTranscripts(t *testing.T) {
	l := line("V1", 100, "Sara", "[transcript] ابعتلي الملف بكره لو سمحت يا اخي")
	l.MediaType = "voice_note"
	c := chunkWith(l)

	if _, _, ok := extract.Verify(c, proposal("#V1", "ابعتلي الملف بكره لو سمحت", "send the file")); !ok {
		t.Errorf("a lightly-trimmed transcript quote should pass")
	}
	if _, reason, ok := extract.Verify(c, proposal("#V1", "حول المبلغ للحساب البنكي فورا", "x")); ok {
		t.Errorf("leniency must not extend to words that were never said (reason %q)", reason)
	}
}

// TestVerifyDropsLowConfidence keeps the review queue worth opening.
func TestVerifyDropsLowConfidence(t *testing.T) {
	c := chunkWith(line("M1", 100, "Sara", "جهز العقد"))
	p := proposal("#M1", "جهز العقد", "x")
	p.Confidence = 0.2
	if _, reason, ok := extract.Verify(c, p); ok || reason != extract.RejectLowScore {
		t.Errorf("a guess below the floor should not reach a human")
	}
}

// TestResolveDueUsesTheMessageDate reproduces the bug that started this
// design. Asked on Monday 7 September 2026 what "الأحد" meant, the model said
// Monday the 14th. The message's own timestamp settles it: the coming Sunday
// is the 13th.
func TestResolveDueUsesTheMessageDate(t *testing.T) {
	loc := riyadh(t)
	monday := time.Date(2026, 9, 7, 11, 0, 0, 0, loc) // a Monday
	if monday.Weekday() != time.Monday {
		t.Fatalf("fixture is wrong: %s is not a Monday", monday)
	}

	got := extract.ResolveDue("الأحد", monday.Unix(), loc)
	if got == 0 {
		t.Fatalf("a weekday name is a resolvable date")
	}
	d := time.Unix(got, 0).In(loc)
	if d.Weekday() != time.Sunday || d.Day() != 13 {
		t.Errorf("الأحد on Mon 7 Sep = %s %d, want Sunday 13", d.Weekday(), d.Day())
	}
}

func TestResolveDuePhrases(t *testing.T) {
	loc := riyadh(t)
	// Tuesday 8 September 2026, 10:00.
	base := time.Date(2026, 9, 8, 10, 0, 0, 0, loc)
	if base.Weekday() != time.Tuesday {
		t.Fatalf("fixture is wrong: %s is not a Tuesday", base)
	}

	cases := []struct {
		phrase  string
		wantDay int
		wantOK  bool
		why     string
	}{
		{"بكرة", 9, true, "tomorrow"},
		{"بكره", 9, true, "tomorrow, spelled the other way"},
		{"tomorrow", 9, true, ""},
		{"اليوم", 8, true, "today"},
		{"الخميس", 10, true, "the coming Thursday"},
		{"الثلاثاء", 15, true, "said on a Tuesday, means next Tuesday, not today"},
		{"next week", 15, true, ""},
		{"end of month", 30, true, "September has 30 days"},
		{"خلال 3 ايام", 11, true, "in three days"},
		{"خلال ٣ ايام", 11, true, "Arabic-Indic digits"},
		{"قريب", 0, false, "a real phrase, but not a date"},
		{"بعد السفر", 0, false, "depends on something we cannot know"},
		{"", 0, false, "nothing said"},
	}
	for _, c := range cases {
		got := extract.ResolveDue(c.phrase, base.Unix(), loc)
		if !c.wantOK {
			if got != 0 {
				t.Errorf("%q should not resolve to a date (%s)", c.phrase, c.why)
			}
			continue
		}
		if got == 0 {
			t.Errorf("%q should resolve (%s)", c.phrase, c.why)
			continue
		}
		if d := time.Unix(got, 0).In(loc); d.Day() != c.wantDay {
			t.Errorf("%q = %s, want day %d (%s)", c.phrase, d.Format("Mon 2 Jan"), c.wantDay, c.why)
		}
	}
}

// TestVerifyDropsFinishedWork — the prompt forbids past-tense reports and
// qwen2.5 returned "عملت شوية تعديلات" ("I made some edits") as a task on
// every run. The rule lives in code because asking did not work.
func TestVerifyDropsFinishedWork(t *testing.T) {
	c := chunkWith(
		line("P1", 100, "Rami", "عملت شوية تعديلات جيدة باذن الله"),
		line("P2", 101, "Sara", "خلصت التقرير امبارح"),
		line("P3", 102, "Sami", "I sent it yesterday"),
		line("P4", 103, "Sara", "عملت التعديلات بس لازم تراجعها انت"),
	)
	for _, id := range []string{"#P1", "#P2", "#P3"} {
		var quote string
		for _, l := range c.Lines {
			if "#"+l.MessageID == id {
				quote = l.Text
			}
		}
		if _, reason, ok := extract.Verify(c, proposal(id, quote, "t")); ok {
			t.Errorf("%s (%q) reports finished work", id, quote)
		} else if reason != extract.RejectFinished {
			t.Errorf("%s: reason = %q, want already_done", id, reason)
		}
	}
	// A report that also asks for something is still a request.
	if _, _, ok := extract.Verify(c, proposal("#P4", "عملت التعديلات بس لازم تراجعها انت", "review")); !ok {
		t.Errorf("a report carrying a request is still a task")
	}
}

// TestVerifyToleratesSmallEdits — models correct while quoting. qwen2.5 wrote
// "الاكسز" where the message said "اكسز", and a real task was thrown away over
// one definite article. The quote must still be anchored to this message, so
// fabrication stays caught.
func TestVerifyToleratesSmallEdits(t *testing.T) {
	c := chunkWith(line("E1", 100, "Karim", "@Ayham ابعتلي اكسز هلقيت قبل منقلع"))

	if _, reason, ok := extract.Verify(c, proposal("#E1", "ابعتلي الاكسز هلقيت قبل منقلع", "send access")); !ok {
		t.Errorf("a one-article edit should not lose the task (reason %q)", reason)
	}
	// Tolerance is not licence: different words are still a fabrication.
	if _, _, ok := extract.Verify(c, proposal("#E1", "حول المبلغ للحساب البنكي اليوم", "x")); ok {
		t.Errorf("words that were never said must still be rejected")
	}
}

// TestVerifyStripsRenderedPrefix — shown "Rami Saleh: وما تنسوا…", the model
// quoted "@Rami Saleh وما تنسوا…", folding the speaker into the message. Four
// correct tasks were rejected as fabrications before this was handled.
func TestVerifyStripsRenderedPrefix(t *testing.T) {
	c := chunkWith(line("R1", 100, "Rami Saleh", "وما تنسوا تقعدوا مع محمود عبد العال"))
	for _, quote := range []string{
		"@Rami Saleh وما تنسوا تقعدوا مع محمود عبد العال",
		"Rami Saleh: وما تنسوا تقعدوا مع محمود عبد العال",
		"[#R1] وما تنسوا تقعدوا مع محمود عبد العال",
	} {
		if _, reason, ok := extract.Verify(c, proposal("#R1", quote, "meet Mahmoud")); !ok {
			t.Errorf("layout copied into the quote should not reject it: %q (%s)", quote, reason)
		}
	}
}

// TestVerifyLeavesMeetingsToTheMeetingsModule — run live on a real group,
// qwen2.5 produced two tasks in a row that were both "agree a time for the
// weekly meeting", despite the prompt forbidding exactly that. Meetings have
// their own module with proper dates, attendees and agendas; a task would be
// the worse copy of the same thing.
func TestVerifyLeavesMeetingsToTheMeetingsModule(t *testing.T) {
	c := chunkWith(
		line("A1", 100, "Ibrahim", "بس لازم نتفق على اجتماع اسبوعي حضوري في جاده"),
		line("A2", 101, "Ibrahim", "ممكن نتفق بالاسابيع القادمه يكون اجتماعنا الاسبوعي السبت من ٥ م"),
		line("A3", 102, "Sara", "لازم تجهز العرض قبل الاجتماع"),
	)

	arranging := []struct{ id, title, quote string }{
		{"#A1", "اقتراح مواعيد للاجتماع الاسبوعي الحضوري", "بس لازم نتفق على اجتماع اسبوعي حضوري في جاده"},
		{"#A2", "تحديد موعد للاجتماع الاسبوعي القادم", "ممكن نتفق بالاسابيع القادمه يكون اجتماعنا الاسبوعي السبت من ٥ م"},
	}
	for _, a := range arranging {
		p := proposal(a.id, a.quote, a.title)
		if _, reason, ok := extract.Verify(c, p); ok {
			t.Errorf("%q is a meeting being arranged, not a task", a.title)
		} else if reason != extract.RejectMeeting {
			t.Errorf("%q: reason = %q, want meeting", a.title, reason)
		}
	}

	// Preparing something for a meeting is work, and stays work.
	if _, reason, ok := extract.Verify(c,
		proposal("#A3", "لازم تجهز العرض قبل الاجتماع", "تجهيز العرض قبل الاجتماع")); !ok {
		t.Errorf("preparing for a meeting is a task (reason %q)", reason)
	}
}

// TestVerifyNeedsEnoughWordsToNameWork — measured against hand-labelled chats,
// the model built "fix the problem in the chat" out of "فيها مشكله" and "check
// that the system works" out of "فل الفل" ("great"). Both quotes matched,
// because the words were really said; the task was invented around them.
func TestVerifyNeedsEnoughWordsToNameWork(t *testing.T) {
	c := chunkWith(
		line("S1", 100, "Tarek", "فيها مشكله"),
		line("S2", 101, "Rami", "فل الفل"),
		line("S3", 102, "Nabil", "وشغلك اخيرة المساعد الذكي"),
	)

	for _, tc := range []struct{ id, quote, title string }{
		{"#S1", "فيها مشكله", "تصحيح المشكلة في الشات"},
		{"#S2", "فل الفل", "تحقق من النظام وتأكد أنه يعمل"},
	} {
		if _, reason, ok := extract.Verify(c, proposal(tc.id, tc.quote, tc.title)); ok {
			t.Errorf("%q is too short to name work", tc.quote)
		} else if reason != extract.RejectTooShort {
			t.Errorf("%q: reason = %q, want too_short", tc.quote, reason)
		}
	}

	// The shortest real task in the labelled set stays in. The floor is set
	// from it, so if it ever falls out the floor is wrong. A short quote out
	// of a long message is fine — it is the message that has to carry work.
	if _, reason, ok := extract.Verify(c,
		proposal("#S3", "وشغلك اخيرة المساعد الذكي", "إضافة المساعد الذكي")); !ok {
		t.Errorf("the shortest real task must survive (reason %q)", reason)
	}
}

// TestVerifyPastTenseCoversMoreVerbs — "عرضت الodoo على عدي ومالك" ("I showed
// Odoo to Adi and Malik") came back as "evaluate the Odoo demo". Showing it
// already happened.
func TestVerifyPastTenseCoversMoreVerbs(t *testing.T) {
	c := chunkWith(line("P2", 100, "Hasan", "عرضت الodoo على عدي ومالك وانهبلوا فيه"))
	if _, reason, ok := extract.Verify(c,
		proposal("#P2", "عرضت الodoo على عدي ومالك وانهبلوا فيه", "تقييم عرض Odoo")); ok {
		t.Errorf("showing it already happened; that is a report")
	} else if reason != extract.RejectFinished {
		t.Errorf("reason = %q, want already_done", reason)
	}
}

package extract

import (
	"regexp"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// Checking a proposed meeting, and turning its words into fields.
//
// The bar is higher than for a task, on purpose. A wrong task is a line in a
// review queue. A wrong meeting carries a date and a list of people, and it
// shows up in the meetings view looking like a commitment somebody made.

// MeetingReject says why a proposed meeting was dropped.
type MeetingReject string

const (
	MRejectEmptyTitle MeetingReject = "empty_title"
	MRejectLowScore   MeetingReject = "low_confidence"
	MRejectNoEvidence MeetingReject = "no_evidence_id" // named a message not in the chunk
	MRejectBadQuote   MeetingReject = "bad_quote"      // the words are not in that message
	MRejectContext    MeetingReject = "context_only"   // the only proof is a carried-in line
	MRejectNotMeeting MeetingReject = "not_meeting"    // no meeting word, no link, no invite
	MRejectAdHoc      MeetingReject = "ad_hoc_call"    // "join now", a bare link
	MRejectPast       MeetingReject = "already_held"   // it happened; nothing to arrange
	MRejectTheirs     MeetingReject = "someone_elses"  // their calendar, not ours
	MRejectDuplicate  MeetingReject = "duplicate"      // already recorded
	MRejectSameEvent  MeetingReject = "same_meeting"   // said twice in one run
	MRejectTooMany    MeetingReject = "run_cap"        // the run had already found plenty
)

// MinMeetingConfidence is the floor. Higher than MinConfidence for tasks
// because the cost of a wrong meeting is higher.
const MinMeetingConfidence = 0.5

// VerifiedMeeting is a proposal that survived checking, with its message.
type VerifiedMeeting struct {
	Meeting ProposedMeeting
	Line    Line
}

// VerifyMeeting checks one proposal against the chunk it came from.
func VerifyMeeting(c Chunk, p ProposedMeeting) (VerifiedMeeting, MeetingReject, bool) {
	if strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 140 {
		return VerifiedMeeting{}, MRejectEmptyTitle, false
	}
	if p.Confidence < MinMeetingConfidence {
		return VerifiedMeeting{}, MRejectLowScore, false
	}

	id := strings.TrimPrefix(strings.TrimSpace(p.EvidenceID), "#")
	var line Line
	found := false
	for _, l := range c.Lines {
		if l.MessageID == id {
			line, found = l, true
			break
		}
	}
	if !found {
		return VerifiedMeeting{}, MRejectNoEvidence, false
	}
	if !quoteMatches(line, p.Evidence) {
		fixed, ok := lineHoldingQuote(c, p.Evidence, line.Sender)
		if !ok {
			return VerifiedMeeting{}, MRejectBadQuote, false
		}
		line = fixed
	}
	// From here the proposal is anchored to a real message, so every rejection
	// carries that message back with it. A drop nobody can trace to a line is
	// a drop nobody can argue with.
	anchored := VerifiedMeeting{Meeting: p, Line: line}

	text := line.Text
	if line.Context {
		// Unlike tasks, a context line is not enough. A task can become obvious
		// only once somebody answers it, so the reply's chunk is allowed to
		// claim it. A meeting is arranged in the open — if the only proof is a
		// line carried in from the chunk before, that chunk already saw it.
		return anchored, MRejectContext, false
	}
	if isPastMeeting(text) {
		return anchored, MRejectPast, false
	}
	if isSomeoneElsesMeeting(text) {
		return anchored, MRejectTheirs, false
	}
	if isAdHocCall(text, p) {
		return anchored, MRejectAdHoc, false
	}
	if !mentionsMeeting(text) && db.MeetingLinkCode(text) == "" && !isCalendarInvite(text) {
		// The model decided a meeting was being arranged, and the message it
		// pointed at says nothing about meeting. That is the shape a
		// fabrication takes here.
		return anchored, MRejectNotMeeting, false
	}
	return anchored, "", true
}

// MentionsMeetingTalk says whether a message reads like it is about meeting.
//
// This is the cheap filter the scanner uses to decide whether a chat is worth
// a model call at all. It is deliberately loose: a false yes costs one model
// call, a false no means a meeting is never found.
func MentionsMeetingTalk(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return mentionsMeeting(text) || db.MeetingLinkCode(text) != "" || isCalendarInvite(text)
}

func mentionsMeeting(text string) bool {
	t := normalise(text)
	for _, n := range meetingNouns {
		if strings.Contains(t, n) {
			return true
		}
	}
	// Verbs of meeting, without the noun: "نتقابل", "نلتقي", "let's meet".
	if containsAny(t,
		"نتقابل", "نلتقى", "نلتقي", "تقابلنا", "نجتمع", "يجتمع", "نتلاقى",
		"let's meet", "lets meet", "meet up", "catch up", "sit down with") {
		return true
	}
	return arrangesTalk(t)
}

// arrangesTalk spots a call set up with no meeting word at all. In Gulf and
// Levant chat this is the usual way: "خلينا نتكلم اليوم بعد العصر", "الليلة
// بنحكي". Nobody says "اجتماع" to a colleague they talk to every week.
//
// A talk verb alone is far too common to count ("بدي احكي معك بموضوع"), so it
// only counts next to a word that says when. The verbs are the forms that
// look ahead — "نحكي", not "حكيت" — so a report of a talk that already
// happened stays out.
func arrangesTalk(normalised string) bool {
	if !containsAny(normalised, talkVerbs...) {
		return false
	}
	if containsAny(normalised, talkTimeWords...) {
		return true
	}
	for day := range weekdayWords {
		if strings.Contains(normalised, day) {
			return true
		}
	}
	return false
}

var talkVerbs = []string{
	"نحكي", "نتكلم", "نتحدث", "نتناقش", "احكي معك", "أحكي معك", "احكي معاك",
	"اكلمك", "أكلمك", "بكلمك", "اتصل فيك", "اتصل بك",
	"let's talk", "lets talk", "let's speak", "lets speak", "have a chat",
	"hop on", "jump on",
}

var talkTimeWords = []string{
	"اليوم", "الليلة", "الليله", "المسا", "مساء", "العصر", "المغرب", "العشا",
	"الصبح", "الصباح", "الظهر", "بكرة", "بكره", "غدا", "غداً", "الساعة", "الساعه",
	"today", "tonight", "tomorrow", "this evening", "this afternoon",
	"this morning", "o'clock",
}

var calendarInviteWords = []string{
	"has invited you to join a video meeting",
	"invited you to a meeting",
	"دعاك للانضمام",
	"google calendar",
	"calendar invite",
	"webcal",
	"ics",
}

func isCalendarInvite(text string) bool {
	return containsAny(normalise(text), calendarInviteWords...)
}

// isPastMeeting spots a meeting talked about after the fact. There is nothing
// left to arrange, so there is nothing to record.
func isPastMeeting(text string) bool {
	t := normalise(text)
	return containsAny(t,
		"كان الاجتماع", "الاجتماع كان", "انتهى الاجتماع", "خلص الاجتماع",
		"بعد الاجتماع الي صار", "التقينا امس", "التقينا البارحة", "اجتمعنا امس",
		"the meeting was", "the meeting went", "after our meeting yesterday",
		"we met yesterday", "good meeting today", "thanks for the meeting")
}

// isSomeoneElsesMeeting spots somebody explaining their own calendar. "عندي
// اجتماع الساعة ٧" is why they cannot answer, not a meeting with you.
func isSomeoneElsesMeeting(text string) bool {
	t := normalise(text)
	mine := containsAny(t,
		"عندي اجتماع", "عندي ميتنج", "عندي موعد", "لدي اجتماع",
		"i have a meeting", "i'm in a meeting", "im in a meeting",
		"عندهم اجتماع", "اجتماعهم", "their meeting", "he has a meeting",
		"she has a meeting", "they have a meeting")
	if !mine {
		return false
	}
	// Unless it turns into an arrangement with the people here: "عندي اجتماع
	// الساعة ٧، نتقابل بعده؟" is both.
	return !containsAny(t,
		"نتقابل", "نجتمع", "نلتقي", "يناسبك", "يناسبكم", "تقدر تحضر",
		"can you join", "can you make it", "shall we meet", "let's meet")
}

// isAdHocCall spots a call starting now rather than a meeting being arranged.
func isAdHocCall(text string, p ProposedMeeting) bool {
	if startsNow(text) {
		return true
	}
	// A message that is only a link, with no day, no time and no purpose, is
	// somebody opening a room — not an arrangement.
	bare := db.MeetingLinkCode(text) != "" &&
		len(strings.Fields(strings.TrimSpace(text))) <= 2 &&
		p.WhenText == "" && p.TimeText == "" && strings.TrimSpace(p.Purpose) == ""
	return bare
}

// startsNow spots a call beginning right away rather than a meeting being
// planned for later: "يلا بانتظارك", a duration of minutes or part of an
// hour coming up, "as soon as you're ready". Shared by the finder
// (isAdHocCall above) and the updater (isAdHocUpdate in meetings_update.go)
// — a call starting now is never a change to a meeting planned for later,
// whichever question is being asked. Round 4, from real data: "انا ممكن
// اكون متاح خلال ١٠ دقايق ربع ساعة" / "تمام وقت ما تكون جاهز ابعتلي".
func startsNow(text string) bool {
	t := westernDigits(normalise(text))
	if containsAny(t,
		"يلا بانتظارك", "بانتظارك", "تفضل", "انا جاهز", "احنا جاهزين",
		"join now", "joining now", "i'm on the call", "im on the call",
		"اتصل فيني", "كلمني الحين", "call me now", "دخلت",
		"وقت ما تكون جاهز", "اول ما تجهز", "ابعتلي لما تجهز",
		"الحين", "هلق", "هسا", "دلوقتي", "right now") {
		return true
	}
	return startsNowSoonPattern.MatchString(t)
}

// startsNowSoonPattern matches a duration coming up — minutes, or a quarter
// or half an hour — over the normalised, digit-folded text. That is a call
// starting soon, not a meeting moved to a later day.
var startsNowSoonPattern = regexp.MustCompile(
	`(خلال|بعد|كمان)\s*(\d+\s*)?(دقايق|دقائق|دقيقة|دقيقه)` +
		`|(خلال|بعد|كمان)\s*(ربع|نص|نصف)\s*ساعة` +
		`|in \d+ ?(min|mins|minutes)` +
		`|in (a|half an|quarter of an) hour`)

// ResolveMeetingStart turns "الخميس" plus "٥ العصر" into a real timestamp, or
// 0 when the words do not name a day.
//
// Sharing ResolveDue would be wrong: a task due "الخميس" lands at the end of
// the working day, which is a sensible default for a deadline and a nonsense
// one for a meeting. A meeting with no clock time gets the day and a neutral
// hour, and the words are kept alongside so the review screen can show what
// was actually said.
func ResolveMeetingStart(whenText, timeText string, sentAt int64, loc *time.Location) int64 {
	if loc == nil {
		loc = time.UTC
	}
	day := resolveMeetingDay(whenText, sentAt, loc)
	if day.IsZero() {
		return 0
	}
	hour, minute, ok := clockTime(timeText)
	if !ok {
		// No exact clock time, but a part of the day is often still named —
		// "الليلة", "بعد العصر" — and that is a far better guess than a flat
		// noon for every unqualified meeting.
		if h, found := partOfDayHour(whenText, timeText); found {
			hour, minute = h, 0
		} else {
			// A day with no time at all. Noon reads as "that day" in every
			// list without pretending to a precision nobody stated.
			hour, minute = 12, 0
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc).Unix()
}

// partOfDayHour reads a named part of the day into a neutral clock hour, for
// when no exact time was said. Ordered latest-in-the-day first, since a
// message naming more than one of these together ("بعد العصر بليل") almost
// always means the later one.
func partOfDayHour(texts ...string) (int, bool) {
	var t string
	for _, s := range texts {
		t += " " + normalise(s)
	}
	switch {
	case containsAny(t, "الليلة", "الليله", "tonight", "بعد العشا"):
		return 21, true
	case containsAny(t, "المسا", "مساء", "المساء", "evening", "بعد المغرب"):
		return 19, true
	case containsAny(t, "بعد العصر", "العصر", "afternoon"):
		return 17, true
	case containsAny(t, "الظهر", "noon"):
		return 13, true
	case containsAny(t, "الصبح", "الصباح", "morning"):
		return 10, true
	}
	return 0, false
}

// ResolveMeetingStartInChunk is ResolveMeetingStart with the two checks that
// stop a date being invented, and it is what the pipeline calls.
//
// Both come from one real failure. A group agreed on 7 September "بلكي بكرة"
// ("maybe tomorrow"), then said nothing for five days and reopened the subject
// on the 12th with "كيف مواعيدكم نرتب اجتماع". The model quoted the 12th
// correctly and reported when_text "بكرة" from the 7th, and the meeting landed
// on the 13th — a date nobody had ever proposed.
//
// So:
//
//  1. The timing words are anchored to the message that actually said them,
//     not to the evidence message. "بكرة" said on the 7th means the 8th.
//  2. A meeting cannot be arranged for a time before the arranging. Once
//     anchored, the 8th is in the past relative to the 12th, so the date is
//     dropped and the words are kept instead.
//
// A dropped date is not a lost meeting. It stays "proposed" with the timing in
// its own words, which is exactly what a meeting still being arranged is.
func ResolveMeetingStartInChunk(c Chunk, v VerifiedMeeting, loc *time.Location) int64 {
	when := v.Meeting.WhenText
	if strings.TrimSpace(when) == "" {
		return 0
	}
	anchor := timingAnchor(c, when, v.Line)
	startsAt := ResolveMeetingStart(when, v.Meeting.TimeText, anchor, loc)
	if startsAt == 0 {
		return 0
	}
	// Arranged for a day before the arranging happened. Not a meeting being
	// set up — stale words picked out of an old message.
	//
	// Whole days, not timestamps: a meeting proposed for "اليوم" at half past
	// three is still today, even though the neutral noon that stands in for an
	// unnamed hour has already gone by.
	if startOfDay(startsAt, loc).Before(startOfDay(v.Line.TS, loc)) {
		return 0
	}
	return startsAt
}

func startOfDay(ts int64, loc *time.Location) time.Time {
	t := time.Unix(ts, 0).In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// timingAnchor finds when the timing words were said. Falls back to the
// evidence message, which is right whenever the model quoted from it.
//
// It looks both ways around the evidence. Availability usually arrives as a
// reply — "نرتب اجتماع" then, a minute later, "انا اليوم متاح" — so the
// message holding the timing words is often AFTER the one that proves the
// meeting, not before it.
func timingAnchor(c Chunk, whenText string, evidence Line) int64 {
	needle := normalise(whenText)
	if needle == "" {
		return evidence.TS
	}
	// The evidence message first: the common case, and it settles the tie when
	// several messages use the same word.
	if strings.Contains(normalise(evidence.Text), needle) {
		return evidence.TS
	}
	// Otherwise the message using those words that sits closest in time to the
	// evidence. Closest, because a thread reopened after a week must not pick
	// up "بكرة" from the week before.
	var best int64
	var bestGap int64 = -1
	for _, l := range c.Lines {
		if !strings.Contains(normalise(l.Text), needle) {
			continue
		}
		gap := l.TS - evidence.TS
		if gap < 0 {
			gap = -gap
		}
		if bestGap < 0 || gap < bestGap {
			best, bestGap = l.TS, gap
		}
	}
	if best > 0 {
		return best
	}
	return evidence.TS
}

// resolveMeetingDay finds the calendar day the words point at. Zero time means
// "no day was named", which is a normal and common answer.
func resolveMeetingDay(whenText string, sentAt int64, loc *time.Location) time.Time {
	t := normalise(whenText)
	if t == "" || sentAt == 0 {
		return time.Time{}
	}
	sent := time.Unix(sentAt, 0).In(loc)

	switch {
	case containsAny(t, "بعد بكرة", "بعد بكره", "day after tomorrow"):
		return sent.AddDate(0, 0, 2)
	case containsAny(t, "اليوم", "today", "النهاردة", "الليلة", "tonight"):
		return sent
	case containsAny(t, "بكرة", "بكره", "غدا", "غدآ", "tomorrow"):
		return sent.AddDate(0, 0, 1)
	}
	if n, ok := numberBefore(t, "days", "ايام", "أيام", "يوم"); ok {
		return sent.AddDate(0, 0, n)
	}
	if n, ok := numberBefore(t, "weeks", "اسابيع", "أسابيع", "اسبوع"); ok {
		return sent.AddDate(0, 0, 7*n)
	}
	if wd, ok := weekday(t); ok {
		diff := (int(wd) - int(sent.Weekday()) + 7) % 7
		if diff == 0 {
			// "الخميس" said on a Thursday means the one coming, unless they
			// said so: "اليوم الخميس" was already handled above.
			diff = 7
		}
		return sent.AddDate(0, 0, diff)
	}
	if containsAny(t, "الاسبوع الجاي", "الأسبوع الجاي", "الاسبوع القادم", "next week") {
		return sent.AddDate(0, 0, 7)
	}
	// "قريب", "بعد السفر", "soon" are real phrases and not days. They are kept
	// as words on the meeting; the date stays empty.
	return time.Time{}
}

// clockTime reads "٥ العصر", "الساعة ٤", "2pm", "14:30" into hours and minutes.
func clockTime(timeText string) (int, int, bool) {
	t := westernDigits(normalise(timeText))
	if t == "" {
		return 0, 0, false
	}

	digits := []rune{}
	nums := []int{}
	flush := func() {
		if len(digits) > 0 {
			n := 0
			for _, d := range digits {
				n = n*10 + int(d-'0')
			}
			nums = append(nums, n)
			digits = digits[:0]
		}
	}
	for _, r := range t {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
			continue
		}
		flush()
	}
	flush()
	if len(nums) == 0 {
		return 0, 0, false
	}

	hour := nums[0]
	minute := 0
	// "14:30" and "4.30" both arrive as two numbers. A second number above 59
	// is not minutes — it is a date or a phone number, and it is ignored.
	if len(nums) > 1 && nums[1] < 60 && strings.ContainsAny(t, ":.٫") {
		minute = nums[1]
	}
	if hour > 23 {
		return 0, 0, false
	}

	// Afternoon words. Arabic names the part of the day rather than am/pm:
	// العصر is mid-afternoon, المغرب is sunset, المساء and الليل are evening.
	pm := containsAny(t, "pm", "p.m", "مساء", "المساء", "العصر", "عصرا", "عصراً",
		"المغرب", "الليل", "ليلا", "ليلاً", "بعد الظهر", "الظهر")
	am := containsAny(t, "am", "a.m", "صباح", "الصباح", "صباحا", "صباحاً", "الفجر")
	switch {
	case pm && hour < 12:
		hour += 12
	case am && hour == 12:
		hour = 0
	case !pm && !am && hour >= 1 && hour <= 7:
		// "الساعة ٥" with no qualifier. Nobody arranges a work meeting for 5am
		// here, and reading it as dawn puts the meeting on the wrong side of
		// the day. The words are kept on the meeting either way.
		hour += 12
	}
	return hour, minute, true
}

// ResolveAttendees turns the names the model read into real people.
//
// A name that matches nobody in the roster is dropped. The alternative —
// inventing a participant row with a name and no JID — puts a person on a
// meeting who may not be the person meant.
func ResolveAttendees(names []string, roster []RosterPerson) []db.MeetingParticipant {
	out := []db.MeetingParticipant{}
	seen := map[string]bool{}
	for _, n := range names {
		p := matchRoster(n, roster)
		if p == nil || seen[p.JID] {
			continue
		}
		seen[p.JID] = true
		out = append(out, db.MeetingParticipant{
			JID:  p.JID,
			Name: p.Name,
			Role: "attendee",
			RSVP: "unknown",
		})
	}
	return out
}

// MeetingTimeOptions is what gets stored in time_options: the timing words as
// they were actually said, as a JSON array. When no date could be computed,
// this is the only record of "قريب" or "بعد السفر", and it is what the review
// screen shows instead of an empty date.
func MeetingTimeOptions(whenText, timeText string) []string {
	out := []string{}
	for _, s := range []string{whenText, timeText} {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// meetingStatus keeps the model's answer only when it is one we know, and only
// when a time was actually named. "confirmed" with no date is a contradiction.
func meetingStatus(p ProposedMeeting, startsAt int64) string {
	if strings.EqualFold(strings.TrimSpace(p.Status), db.MeetingConfirmed) && startsAt > 0 {
		return db.MeetingConfirmed
	}
	return db.MeetingProposed
}

// meetingMode normalises the mode, and infers it when the chat did not say:
// a join link means online, a named place means in person.
func meetingMode(p ProposedMeeting) string {
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "online", "remote", "video", "call":
		return "online"
	case "in_person", "in-person", "onsite", "physical":
		return "in_person"
	}
	if strings.TrimSpace(p.Link) != "" {
		return "online"
	}
	if strings.TrimSpace(p.Location) != "" {
		return "in_person"
	}
	return ""
}

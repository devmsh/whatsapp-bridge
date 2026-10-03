package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/llmlog"
)

// Keeping a meeting up to date: checking the model's "what changed?" answers,
// turning them into a database patch, and the per-chat run that ties it all
// together. Same shape as meetings_run.go and meetings_verify.go, one step
// later: those two FIND a meeting once, this one watches it afterwards.

// UpdateReject says why a proposed update was dropped.
type UpdateReject string

const (
	URejectUnknownRef UpdateReject = "unknown_ref" // named a ref not in this call
	URejectLowScore   UpdateReject = "low_confidence"
	URejectNoEvidence UpdateReject = "no_evidence_id" // named a message not in the chunk
	URejectBadQuote   UpdateReject = "bad_quote"      // the words are not in that message
	URejectContext    UpdateReject = "context_only"   // the only proof is a carried-in line
	URejectAdHoc      UpdateReject = "ad_hoc_call"    // a call starting now, not a planned change
	URejectBadStatus  UpdateReject = "unknown_status" // not "" or one of the known statuses
	// URejectResolvedNoNote: "resolved" with no note is a status with no
	// record of WHAT was decided, which defeats the point of the status.
	URejectResolvedNoNote UpdateReject = "resolved_no_note"
	URejectEmpty          UpdateReject = "empty_update" // nothing in it would actually change anything
	URejectTooEarly       UpdateReject = "too_early_to_be_held"
	// URejectNotNew: the evidence is a message the meeting already knew
	// about — the one that created it, or one already read.
	URejectNotNew UpdateReject = "not_new"
	// URejectSecondOpinion: the second opinion said no. See UpdateClaim.
	URejectSecondOpinion UpdateReject = "second_opinion_no"
)

// MinUpdateConfidence is the floor for a proposed update, same value the
// prompt itself tells the model to cut off at.
const MinUpdateConfidence = 0.6

// MinTimingConfidence is the higher floor for an update that touches the
// meeting's date or time (when_text, time_text or postponed). A wrong date
// is worse than a missed one, so timing gets held to the prompt's "clear"
// tier (0.7), not just "not sure" (0.6).
const MinTimingConfidence = 0.7

// VerifiedUpdate is a proposal that survived checking, with the message that
// proves it and the known meeting it is about.
type VerifiedUpdate struct {
	Update ProposedUpdate
	Line   Line
	Known  KnownMeeting
	// Chunk is the chunk the update was checked against. BuildPatch needs it
	// to resolve WhenText the same stale-words-safe way the finder does (see
	// ResolveMeetingStartInChunk) — the words can sit in an earlier message
	// than the evidence line.
	Chunk Chunk
}

// VerifyUpdate checks one proposed update against the chunk it came from and
// the list of known meetings it was allowed to talk about.
func VerifyUpdate(c Chunk, known []KnownMeeting, u ProposedUpdate) (VerifiedUpdate, UpdateReject, bool) {
	// The model sometimes puts the identical word in both timing fields
	// ("الليلة" as when_text AND time_text). Read that as one signal, not a
	// specific clock time on top of a day.
	if wt, tt := strings.TrimSpace(u.WhenText), strings.TrimSpace(u.TimeText); tt != "" && tt == wt {
		u.TimeText = ""
	}

	var k KnownMeeting
	foundRef := false
	for _, kn := range known {
		if kn.Ref == u.Ref {
			k, foundRef = kn, true
			break
		}
	}
	if !foundRef {
		return VerifiedUpdate{}, URejectUnknownRef, false
	}

	// A wrong date or time is costlier than a wrong status or agenda line, so
	// anything that touches the meeting's timing needs to clear the prompt's
	// "clear" tier, not just "not sure".
	minConfidence := MinUpdateConfidence
	if strings.TrimSpace(u.WhenText) != "" || strings.TrimSpace(u.TimeText) != "" || u.Postponed {
		minConfidence = MinTimingConfidence
	}
	if u.Confidence < minConfidence {
		return VerifiedUpdate{}, URejectLowScore, false
	}

	id := strings.TrimPrefix(strings.TrimSpace(u.EvidenceID), "#")
	var line Line
	foundLine := false
	for _, l := range c.Lines {
		if l.MessageID == id {
			line, foundLine = l, true
			break
		}
	}
	if !foundLine {
		return VerifiedUpdate{}, URejectNoEvidence, false
	}
	if !quoteMatches(line, u.Evidence) {
		fixed, ok := lineHoldingQuote(c, u.Evidence, line.Sender)
		if !ok {
			return VerifiedUpdate{}, URejectBadQuote, false
		}
		line = fixed
	}

	// Clean the agenda before it can change anything or count toward "empty":
	// drop a line that just echoes the evidence quote back, anything
	// absurdly long, and cap how many lines one update can add at once.
	u.AgendaAdd = filterAgendaLines(u.AgendaAdd, c, line.Text)

	// Timing words only count when the evidence message itself says them.
	// Seen on real chats: "send the feedback by Friday" sat in the same slice
	// as a planned meeting, and the model moved the meeting to Friday with a
	// note about feedback. A change of time is proved by the message that
	// names the time, or it is not proved.
	if !saidIn(line.Text, u.WhenText) {
		u.WhenText = ""
	}
	if !saidIn(line.Text, u.TimeText) {
		u.TimeText = ""
	}

	// A meeting that already happened is not rescheduled by later talk about
	// the same people. Live bug: "نجلس الأحد قبل اجتماع orbit نشوف قصة
	// الQRT" moved the dates of TWO meetings whose own dates had already
	// passed — it was about a third meeting. New talk once a meeting's date
	// is well behind it is a new meeting, which the finder handles, not a
	// change to the old one. Status held/cancelled/resolved still pass: a
	// meeting can always be closed out after the fact.
	if k.StartsAt > 0 && line.TS > k.StartsAt+24*3600 {
		u.WhenText = ""
		u.TimeText = ""
		u.Postponed = false
	}

	// The join link must really be in this message — a link quoted from
	// somewhere else is not proof about this meeting's room.
	if link := strings.TrimSpace(u.Link); link != "" {
		code := db.MeetingLinkCode(link)
		if code == "" || !strings.Contains(strings.ToLower(line.Text), code) {
			link = ""
		} else if isBareLinkMessage(line.Text) &&
			(k.StartsAt == 0 || line.TS < k.StartsAt-45*60 || line.TS > k.StartsAt+90*60) {
			// Live bug: three IN-PERSON meetings in another city each got a Google
			// Meet link because somebody posted a bare link, days away from
			// the meeting, and the model tied it to whichever meeting was
			// open. A message that is ONLY a link belongs to a meeting only
			// when that meeting is dated and the link arrived close to its
			// own time — the one case people really do post the room link
			// right before the meeting. Otherwise it is a call starting now.
			link = ""
		}
		u.Link = link
	}

	// The mode must be said, not guessed from a link or a place that happens
	// to be nearby. A link alone can still make an UNDATED-mode meeting
	// online — see BuildPatch — but it never overrides a mode already named.
	if u.Mode != "" && !modeSaidIn(line.Text) {
		u.Mode = ""
	}

	// From here the proposal is anchored to a real message, so every
	// rejection from now on carries that message back with it.
	anchored := VerifiedUpdate{Update: u, Line: line, Known: k, Chunk: c}

	if line.Context {
		return anchored, URejectContext, false
	}

	if isAdHocUpdate(line.Text, u.TimeText) {
		// A call starting now or in a few minutes is not a change to a
		// meeting that was planned for later, even about the same people.
		return anchored, URejectAdHoc, false
	}

	status := strings.ToLower(strings.TrimSpace(u.Status))
	switch status {
	case "", db.MeetingProposed, db.MeetingConfirmed, db.MeetingCancelled, db.MeetingHeld, db.MeetingResolved:
	default:
		return anchored, URejectBadStatus, false
	}

	if status == db.MeetingResolved && strings.TrimSpace(u.Note) == "" {
		// "resolved" with no note is a status with no record of what was
		// actually decided, which defeats the point of the status.
		return anchored, URejectResolvedNoNote, false
	}

	if status == "" && strings.TrimSpace(u.WhenText) == "" && strings.TrimSpace(u.TimeText) == "" &&
		!u.Postponed && strings.TrimSpace(u.Mode) == "" && strings.TrimSpace(u.Location) == "" &&
		strings.TrimSpace(u.Link) == "" && len(u.AgendaAdd) == 0 {
		// Nothing here would change the meeting. The model is meant to answer
		// this way most of the time, but it must not be written as if it were
		// an update.
		return anchored, URejectEmpty, false
	}

	if status == db.MeetingHeld && k.StartsAt > 0 && line.TS < k.StartsAt-3600 {
		// A meeting cannot have taken place before its own start, give or
		// take an hour for people who say "الاجتماع تمام" a little early.
		return anchored, URejectTooEarly, false
	}

	return anchored, "", true
}

// saidIn reports whether any real word of phrase appears in text. The model
// may tidy the words a little ("الساعة ٥" for "٥"), so one shared word is
// enough, with Arabic-Indic digits read the same as Latin ones. An empty
// phrase is not said.
func saidIn(text, phrase string) bool {
	hay := westernDigits(normalise(text))
	for _, w := range strings.Fields(westernDigits(normalise(phrase))) {
		if w == "الساعة" || w == "الساعه" || w == "يوم" || w == "at" || w == "on" {
			continue // glue words that prove nothing on their own
		}
		if strings.Contains(hay, w) {
			return true
		}
	}
	return false
}

// maxAgendaLinesPerUpdate and maxAgendaLineRunes bound what one update can do
// to the agenda in one go, so one busy exchange cannot flood it (round 2,
// part B2 — seen live: the model added general chat as an agenda point).
const (
	maxAgendaLinesPerUpdate = 3
	maxAgendaLineRunes      = 140
)

// filterAgendaLines drops what should never have been proposed as an agenda
// point: the update's own evidence quote read back as if it were a point,
// anything unreasonably long, anything past the per-update cap, and — round
// 4 — anything not actually grounded in the real chat. Live bug: the model
// saved the PROMPT'S OWN example phrases as agenda lines, twelve times,
// because they read like ordinary Arabic and nothing checked that they were
// ever really said in this chat.
func filterAgendaLines(lines []string, c Chunk, evidenceText string) []string {
	evidenceNorm := normalise(evidenceText)
	haystack := chunkWords(c)
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || len([]rune(l)) > maxAgendaLineRunes || normalise(l) == evidenceNorm {
			continue
		}
		if !groundedInChat(l, haystack) {
			continue
		}
		out = append(out, l)
		if len(out) == maxAgendaLinesPerUpdate {
			break
		}
	}
	return out
}

// chunkWords is the normalised, digit-folded text of a chunk's real
// (non-context) lines, joined into one string to search against. Context
// lines do not count — they were carried in only so a reply makes sense, not
// because this chunk is vouching for them.
func chunkWords(c Chunk) string {
	var b strings.Builder
	for _, l := range c.Lines {
		if l.Context {
			continue
		}
		b.WriteString(" ")
		b.WriteString(westernDigits(normalise(l.Text)))
	}
	return b.String()
}

// groundedInChat reports whether at least half of a line's real words (3+
// letters, after normalise and digit-folding) show up somewhere in the
// chat's own words. A line with no such words at all is dropped outright —
// it cannot be grounded in anything.
func groundedInChat(line, haystack string) bool {
	words := strings.Fields(westernDigits(normalise(line)))
	var real, found int
	for _, w := range words {
		if len([]rune(w)) < 3 {
			continue
		}
		real++
		if strings.Contains(haystack, w) {
			found++
		}
	}
	if real == 0 {
		return false
	}
	return found*2 >= real
}

// isAdHocUpdate spots the case seen on real chats: "لقاء الأحد" bent into
// "today, in 10 minutes" because somebody said something that starts a call
// right now, not a change to a meeting planned for later. startsNow is
// shared with the finder's isAdHocCall (meetings_verify.go, round 4).
func isAdHocUpdate(evidenceText, timeText string) bool {
	if startsNow(evidenceText) {
		return true
	}
	return strings.TrimSpace(timeText) != "" && startsNow(timeText)
}

// urlPattern strips a URL out of a message so what is left can be counted as
// real words, for the "a message that is only a link" test below.
var urlPattern = regexp.MustCompile(`\S*://\S+`)

// isBareLinkMessage reports whether a message is basically just a link: take
// the URL out, and 2 words or fewer are left.
func isBareLinkMessage(text string) bool {
	stripped := urlPattern.ReplaceAllString(text, "")
	return len(strings.Fields(stripped)) <= 2
}

// onlineModeWords and inPersonModeWords are the words that actually say a
// meeting's mode, as opposed to code guessing it from a link or a place.
var onlineModeWords = []string{
	"اونلاين", "أونلاين", "online", "عن بعد", "زوم", "zoom", "meet", "ميت",
	"جوجل ميت", "فيديو",
}
var inPersonModeWords = []string{
	"حضوري", "وجاهي", "in person", "بالمكتب", "نتقابل", "عندي", "عندك",
}

// modeSaidIn reports whether the evidence message itself names a mode.
func modeSaidIn(text string) bool {
	t := normalise(text)
	return containsAny(t, onlineModeWords...) || containsAny(t, inPersonModeWords...)
}

// setTimeOptionsIfChanged sets p.TimeOptions to the JSON of the new timing
// words, unless that is exactly what the meeting already has on record — a
// patch that changes nothing is not worth a meeting_changes row.
func setTimeOptionsIfChanged(p *db.MeetingPatch, current db.Meeting, whenText, timeText string) {
	options, _ := json.Marshal(MeetingTimeOptions(whenText, timeText))
	optionsStr := string(options)
	if optionsStr != current.TimeOptions {
		p.TimeOptions = &optionsStr
	}
}

// BuildPatch turns a verified update into a db.MeetingPatch. current is the
// meeting as it stands right now (read fresh, in case an earlier chunk in the
// same run already changed it).
func BuildPatch(v VerifiedUpdate, current db.Meeting, loc *time.Location, runID, chatJID string) db.MeetingPatch {
	if loc == nil {
		loc = time.UTC
	}
	u := v.Update
	p := db.MeetingPatch{
		Note:       u.Note,
		Source:     db.ChangeByModel,
		ChatJID:    chatJID,
		MessageID:  v.Line.MessageID,
		EvidenceTS: v.Line.TS,
		RunID:      runID,
	}

	switch {
	case strings.TrimSpace(u.WhenText) != "":
		// Reuse the finder's own stale-words protection (see
		// ResolveMeetingStartInChunk): the day words can sit in an earlier
		// message than the one that proves the change, and a date anchored
		// to that earlier message must not land before the evidence itself
		// was said.
		start := ResolveMeetingStartInChunk(v.Chunk, VerifiedMeeting{
			Meeting: ProposedMeeting{WhenText: u.WhenText, TimeText: u.TimeText},
			Line:    v.Line,
		}, loc)
		if start == 0 && current.StartsAt > 0 && !u.Postponed {
			// The new words name no day code can work out ("بعد السفر"), and
			// nobody said the meeting is off. A date already agreed is worth
			// more than words that replace it with nothing, so it stays.
			// Seen on real chats: a note saying "the date was confirmed"
			// next to a date wiped to zero.
			break
		}
		if start != 0 && current.StartsAt > 0 &&
			startOfDay(start, loc).Equal(startOfDay(current.StartsAt, loc)) {
			// Same day, but did the words actually give a time, or did the
			// resolver fall back to its own neutral hour (noon, or a
			// part-of-day guess)? Check the exact same two functions the
			// resolver itself falls back through.
			_, _, gotClock := clockTime(u.TimeText)
			_, gotPartOfDay := partOfDayHour(u.WhenText, u.TimeText)
			if !gotClock && !gotPartOfDay {
				// A vague "still today" on the day the meeting is already
				// set for must not overwrite a real, exact time with the
				// resolver's neutral noon.
				break
			}
		}
		p.StartsAt = &start
		setTimeOptionsIfChanged(&p, current, u.WhenText, u.TimeText)
	case strings.TrimSpace(u.TimeText) != "" && current.StartsAt > 0:
		// Only the clock time moved; the day the meeting is already on does
		// not. ResolveMeetingStart has no way to say "keep this day", so the
		// new hour is read with the same parser it already uses
		// (clockTime) and placed on the meeting's existing calendar day.
		hour, minute, ok := clockTime(u.TimeText)
		if !ok {
			// "الليلة" and "المسا" name no clock time, but they do move a
			// meeting that was set for the afternoon. The same neutral hours
			// the resolver uses for a part of the day apply here.
			if h, found := partOfDayHour(u.TimeText); found {
				hour, minute, ok = h, 0, true
			}
		}
		if ok {
			day := time.Unix(current.StartsAt, 0).In(loc)
			start := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc).Unix()
			p.StartsAt = &start
			// The recorded words must move with the clock, or time_options
			// keeps saying the old time once starts_at has already changed.
			setTimeOptionsIfChanged(&p, current, "", u.TimeText)
		}
	}

	status := strings.ToLower(strings.TrimSpace(u.Status))
	if u.Postponed && strings.TrimSpace(u.WhenText) == "" {
		zero := int64(0)
		p.StartsAt = &zero
		// Force "proposed" only when the model did not already name a
		// closing status: "نأجلها" next to "خلص مش لازم" must not turn a
		// real cancellation back into an open meeting.
		if status == "" || status == db.MeetingProposed {
			status = db.MeetingProposed
		}
	}

	// "confirmed needs a date" is checked against the EFFECTIVE status — the
	// model's new status when it gave one, otherwise the meeting's current
	// status — not only a status this particular update happens to set.
	// Clearing a confirmed meeting's date (when_text names no resolvable
	// day) must also drop it back to proposed, even with status: "".
	resultingStartsAt := current.StartsAt
	if p.StartsAt != nil {
		resultingStartsAt = *p.StartsAt
	}
	effective := status
	if effective == "" {
		effective = current.Status
	}
	if effective == db.MeetingConfirmed && resultingStartsAt == 0 {
		// Same rule as meetingStatus: "confirmed" with no date is a
		// contradiction, so it falls back to "proposed".
		status = db.MeetingProposed
	}
	if status != "" {
		p.Status = &status
	}

	if mode := strings.TrimSpace(u.Mode); mode != "" {
		p.Mode = &mode
	} else if strings.TrimSpace(u.Link) != "" && current.Mode == "" {
		// A link alone never changes a mode that is already set (mode above
		// only ever comes from words the evidence itself says) — but a
		// meeting with no mode yet, given a link that survived verification,
		// is online, same as the finder infers for a brand new meeting.
		online := "online"
		p.Mode = &online
	}
	if location := strings.TrimSpace(u.Location); location != "" {
		p.Location = &location
	}
	if link := strings.TrimSpace(u.Link); link != "" {
		p.Link = &link
	}
	// A meeting that is being closed needs no new agenda. The model likes to
	// restate the decision as an agenda point next to "resolved"; the note
	// already holds it.
	closing := status == db.MeetingResolved || status == db.MeetingCancelled || status == db.MeetingHeld
	if len(u.AgendaAdd) > 0 && !closing {
		p.AgendaAdd = u.AgendaAdd
	}
	return p
}

// updateNeedsVerdict says whether an update carries a claim worth a second
// opinion — a status, a timing change, a postponement, or a link (round 4:
// live bug, three in-person meetings each got a Google Meet link tied to
// them by a bare link message days away). Agenda, mode and location are the
// chat's own words, copied, not a judgement call, so they never need one.
func updateNeedsVerdict(u ProposedUpdate) bool {
	return strings.TrimSpace(u.Status) != "" || strings.TrimSpace(u.WhenText) != "" ||
		strings.TrimSpace(u.TimeText) != "" || u.Postponed || strings.TrimSpace(u.Link) != ""
}

// hasNonJudgedContent says whether an update still has something worth
// applying once everything the second opinion covers — status, timing,
// postponed, link — is dropped.
func hasNonJudgedContent(u ProposedUpdate) bool {
	return strings.TrimSpace(u.Mode) != "" || strings.TrimSpace(u.Location) != "" || len(u.AgendaAdd) > 0
}

// claimForUpdate turns an update into the one plain English sentence the
// second opinion is asked to check — built by CODE, from the update's own
// fields, never phrased by the model that made the claim in the first place.
func claimForUpdate(u ProposedUpdate) string {
	var parts []string

	when := strings.TrimSpace(u.WhenText)
	clock := strings.TrimSpace(u.TimeText)
	if when != "" || clock != "" {
		parts = append(parts, "The meeting is moved to: "+strings.TrimSpace(when+" "+clock)+".")
	}
	if u.Postponed && when == "" {
		parts = append(parts, "The meeting is postponed with no new day.")
	}

	switch strings.ToLower(strings.TrimSpace(u.Status)) {
	case db.MeetingProposed:
		parts = append(parts, "The meeting is proposed again, not yet confirmed.")
	case db.MeetingConfirmed:
		parts = append(parts, "The meeting is confirmed.")
	case db.MeetingCancelled:
		parts = append(parts, "The meeting is cancelled.")
	case db.MeetingHeld:
		parts = append(parts, "The meeting already took place.")
	case db.MeetingResolved:
		parts = append(parts,
			"The meeting is no longer needed because the chat already settled it: "+strings.TrimSpace(u.Note)+".")
	}

	if link := strings.TrimSpace(u.Link); link != "" {
		parts = append(parts, "The join link for this meeting is: "+link+".")
	}

	return strings.Join(parts, " ")
}

// evidenceWindow renders the evidence line in its own short context — up to
// 3 rendered lines before it and 3 after, in order — rather than handing the
// second opinion the whole chunk. The claim is about ONE message; a narrow
// window is what stops a claim that quotes one line from being excused by
// something unrelated said three days earlier in the same chunk.
func evidenceWindow(c Chunk, line Line, loc *time.Location) string {
	idx := -1
	for i, l := range c.Lines {
		if l.MessageID == line.MessageID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// The line came from lineHoldingQuote and, oddly, is not found by id
		// in its own chunk — fall back to the line alone rather than fail.
		return Render(Chunk{ChatJID: c.ChatJID, Lines: []Line{line}}, loc)
	}
	start := idx - 3
	if start < 0 {
		start = 0
	}
	end := idx + 4 // +1 for the line itself, +3 after
	if end > len(c.Lines) {
		end = len(c.Lines)
	}
	return Render(Chunk{ChatJID: c.ChatJID, Lines: c.Lines[start:end]}, loc)
}

// UpdateDeps is what a refresh run needs.
type UpdateDeps struct {
	Store   *db.Store
	Updater MeetingUpdater
	Loc     *time.Location
	DryRun  bool
	// RecentSince, when set, limits the run to meetings whose unread window
	// starts at or after this time. A run reads a chat oldest first, so in a
	// chat with months of open meetings a meeting from this week would wait
	// behind all of them. The caller runs the recent ones first with this
	// set, then everything with it at zero.
	RecentSince int64
}

// RefreshResult is what one chat's refresh run did.
type RefreshResult struct {
	// Meetings is how many open meetings in this chat were due a re-read.
	Meetings int
	Chunks   int
	Calls    int
	Proposed int
	Applied  int
	// Changes is the total number of meeting_changes rows written across
	// every applied update — one update can touch several fields at once.
	Changes int
	// Judged is how many updates were sent to the second opinion. Only some
	// of Proposed reach it — an agenda-only or mode-only update never needs
	// one (see updateNeedsVerdict).
	Judged       int
	Rejections   map[UpdateReject]int
	FailedChunks int
}

// maxFollowUpLines is the same cap RunMeetings would apply if this were the
// finder: at most this many lines are read in one run, oldest first, and the
// rest waits for the next run — because the watermark only moves as far as
// was actually read.
const maxFollowUpLines = 600

// maxKnownPerBatch is how many known meetings ride along in one model call.
//
// One. It began as six, to save calls, and the live run showed what that
// costs: given two meetings, the local model found the right message ("انا
// شايف انو لازم نجازف") and the right outcome (settled in chat), and pinned
// both on the WRONG meeting's number. Code cannot catch that — the quote is
// real and the status is valid. With one meeting per call there is no number
// to get wrong. A call takes about three seconds, and only chats with an open
// meeting are read, so the price is small.
const maxKnownPerBatch = 1

// RefreshChatMeetings reads what changed, for every open meeting in one chat
// that is due a re-read.
func RefreshChatMeetings(ctx context.Context, d UpdateDeps, chatJID, runID string, progress func(string)) (RefreshResult, error) {
	res := RefreshResult{Rejections: map[UpdateReject]int{}}
	say := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	if d.Loc == nil {
		d.Loc = time.UTC
	}
	if d.Updater == nil {
		return res, fmt.Errorf("this engine cannot update meetings")
	}
	// A second opinion is optional: only some engines implement it (see
	// meetings_update_port.go), and a fake used in a test usually will not.
	judge, _ := d.Updater.(MeetingUpdateJudge)
	if d.Store.IsChatExcludedFromAI(chatJID) {
		// Same guard RunMeetings uses. ChatsWithMeetingFollowUps already
		// filters excluded chats, but this is also reachable directly with an
		// explicit chat_jid or meeting_id, which must not bypass it.
		return res, fmt.Errorf("chat is hidden, archived or deleted")
	}

	now := time.Now().Unix()
	ups, err := d.Store.MeetingFollowUpsForChat(chatJID, now)
	if err != nil {
		return res, fmt.Errorf("follow-ups: %w", err)
	}
	if d.RecentSince > 0 {
		recent := ups[:0:0]
		for _, u := range ups {
			if u.Since >= d.RecentSince {
				recent = append(recent, u)
			}
		}
		ups = recent
	}
	if len(ups) == 0 {
		say("no meetings due a re-read in this chat")
		return res, nil
	}
	res.Meetings = len(ups)

	// What each meeting already knew when this run began: nothing at or
	// before this time can count as news about it.
	knownUpTo := map[int64]int64{}
	for _, u := range ups {
		knownUpTo[u.Meeting.ID] = u.Since
	}

	since := ups[0].Since
	until := ups[0].Until
	for _, u := range ups[1:] {
		if u.Since < since {
			since = u.Since
		}
		if u.Until > until {
			until = u.Until
		}
	}
	if until > now {
		until = now
	}

	lines, err := FetchLines(d.Store, chatJID, since, until)
	if err != nil {
		return res, fmt.Errorf("read chat: %w", err)
	}
	if len(lines) == 0 {
		say("nothing new since the last check")
		return res, nil
	}
	if len(lines) > maxFollowUpLines {
		// Oldest first, so the watermark still moves forward on a busy chat
		// instead of skipping straight to the end and never catching up.
		lines = lines[:maxFollowUpLines]
		say("more than %d lines waiting; reading the oldest %d, the rest next run",
			maxFollowUpLines, maxFollowUpLines)
	}

	people, ownName, isGroup, err := Roster(d.Store, chatJID)
	if err != nil {
		return res, fmt.Errorf("roster: %w", err)
	}
	chatName := chatDisplayName(d.Store, chatJID)
	today := time.Now().In(d.Loc).Format("2006-01-02")

	chunks := Split(lines, chatJID, DefaultChunkOptions(d.Loc))
	res.Chunks = len(chunks)
	say("%d new messages in %d chunk(s), %d meeting(s) to check", len(lines), len(chunks), len(ups))

	// stalled marks a meeting whose model call, or whose patch, failed
	// somewhere earlier in THIS run. SetMeetingChecked is a MAX(), so a later
	// chunk's success would otherwise push the watermark past messages the
	// model never actually answered for that meeting — this is what stops
	// that. Once a meeting is stalled, it is done for this run; it is read
	// again from the true watermark on the next one.
	stalled := map[int64]bool{}

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		first, last := chunkSpan(chunk)
		if first == 0 || last == 0 {
			continue
		}

		var alive []db.Meeting
		for _, u := range ups {
			if stalled[u.Meeting.ID] {
				continue
			}
			if !(u.Since < last && u.Until >= first) {
				continue
			}
			// Re-read from the store: an earlier chunk in this same run may
			// have already changed or closed this meeting, and the model
			// must always see the freshest state.
			m, err := d.Store.GetMeeting(u.Meeting.ID)
			if err != nil || m == nil {
				continue
			}
			if m.Status != db.MeetingProposed && m.Status != db.MeetingConfirmed {
				continue
			}
			alive = append(alive, *m)
		}
		if len(alive) == 0 {
			continue
		}

		for start := 0; start < len(alive); start += maxKnownPerBatch {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			end := start + maxKnownPerBatch
			if end > len(alive) {
				end = len(alive)
			}
			batch := alive[start:end]
			known := buildKnownMeetings(batch, d.Loc)

			callCtx := llmlog.With(ctx, llmlog.Tag{
				RunID: runID, Service: "meeting-updates", ChatJID: chatJID,
			})
			started := time.Now()
			out, err := d.Updater.UpdateMeetings(callCtx, MeetingUpdateInput{
				Chunk: chunk, Known: known, Roster: people, OwnName: ownName,
				ChatName: chatName, IsGroup: isGroup, Today: today,
			})
			res.Calls++
			if err != nil {
				say("chunk %d: a batch of %d meeting(s) failed: %v", chunk.Index, len(batch), err)
				res.FailedChunks++
				for _, m := range batch {
					stalled[m.ID] = true
				}
				continue // the watermark for this batch's meetings does not move
			}

			kept := 0
			for _, p := range out.Updates {
				res.Proposed++
				v, reason, ok := VerifyUpdate(chunk, known, p)
				if !ok {
					res.Rejections[reason]++
					continue
				}
				// A change has to come from a message newer than what this
				// meeting already knows. The window starts AT the last known
				// message, so that message sits in the first chunk as an
				// ordinary line. Seen live: the model "resolved" a meeting
				// using the very voice note that created it as proof, and the
				// second opinion agreed.
				if v.Line.TS <= knownUpTo[v.Known.ID] {
					res.Rejections[URejectNotNew]++
					continue
				}

				// The second opinion: a narrower, different question, asked
				// only about the parts code cannot check on its own — a
				// status or a timing change. Agenda, mode, location and link
				// are the chat's own words, copied, not a judgement call.
				if judge != nil && updateNeedsVerdict(v.Update) {
					judgeCtx := llmlog.With(ctx, llmlog.Tag{
						RunID: runID, Service: "meeting-update-judge", ChatJID: chatJID,
					})
					verdict, jerr := judge.JudgeUpdate(judgeCtx, UpdateClaim{
						Known: v.Known, Claim: claimForUpdate(v.Update),
						Evidence: evidenceWindow(v.Chunk, v.Line, d.Loc), Today: today,
						Settled: strings.EqualFold(strings.TrimSpace(v.Update.Status), db.MeetingResolved),
					})
					res.Calls++
					res.Judged++
					if jerr != nil {
						say("chunk %d: second opinion failed for meeting %d: %v",
							chunk.Index, v.Known.ID, jerr)
						res.FailedChunks++
						stalled[v.Known.ID] = true
						continue
					}
					if !verdict.Agrees {
						res.Rejections[URejectSecondOpinion]++
						if !hasNonJudgedContent(v.Update) {
							// Nothing survives the claim being wrong.
							continue
						}
						// The status/timing/link claim was wrong, but the
						// chat still gave a real place or agenda point —
						// those stand on their own and still apply.
						v.Update.Status = ""
						v.Update.WhenText = ""
						v.Update.TimeText = ""
						v.Update.Postponed = false
						v.Update.Link = ""
					}
				}

				cur, err := d.Store.GetMeeting(v.Known.ID)
				if err != nil || cur == nil {
					// Cannot confirm this meeting's current state, so the
					// patch cannot be trusted either — same as a failed
					// call, this meeting is done for this run.
					stalled[v.Known.ID] = true
					continue
				}
				patch := BuildPatch(v, *cur, d.Loc, runID, chatJID)
				res.Applied++
				kept++
				if d.DryRun {
					continue
				}
				changes, err := d.Store.ApplyMeetingPatch(v.Known.ID, patch)
				if err != nil {
					say("could not apply update to meeting %d: %v", v.Known.ID, err)
					stalled[v.Known.ID] = true
					continue
				}
				res.Changes += len(changes)
			}
			say("chunk %d: %d proposed, %d kept", chunk.Index, len(out.Updates), kept)

			if !d.DryRun {
				RecordCall(d.Store, RunMeta{RunID: runID, Engine: d.Updater.Name(), ChatJID: chatJID},
					"meeting-updates", chunk, time.Since(started), len(out.Updates), kept, len(out.Updates)-kept)
				for _, m := range batch {
					if stalled[m.ID] {
						continue
					}
					_ = d.Store.SetMeetingChecked(m.ID, last)
				}
			}
		}
	}

	return res, nil
}

// chunkSpan is the first and last timestamp of a chunk's REAL messages —
// what the brief calls firstLineTS/lastLineTS. Context lines (carried in only
// so a reply makes sense) do not count: they were already read by an earlier
// chunk.
func chunkSpan(c Chunk) (first, last int64) {
	for _, l := range c.Lines {
		if l.Context {
			continue
		}
		if first == 0 || l.TS < first {
			first = l.TS
		}
		if l.TS > last {
			last = l.TS
		}
	}
	return
}

// buildKnownMeetings turns the meetings alive in one chunk into what the
// model is told about them.
func buildKnownMeetings(ms []db.Meeting, loc *time.Location) []KnownMeeting {
	out := make([]KnownMeeting, 0, len(ms))
	for i, m := range ms {
		var agenda []string
		for _, it := range m.Items {
			if it.Kind == db.ItemAgenda {
				agenda = append(agenda, it.Text)
			}
		}
		var attendees []string
		for _, part := range m.Participants {
			name := part.Name
			if name == "" {
				name = part.JID
			}
			attendees = append(attendees, name)
		}
		out = append(out, KnownMeeting{
			Ref: i + 1, ID: m.ID, Title: m.Title, Purpose: m.Purpose, Status: m.Status,
			When: renderKnownWhen(m, loc), Mode: m.Mode, Location: m.Location, Link: m.Link,
			Agenda: agenda, Attendees: attendees, StartsAt: m.StartsAt,
		})
	}
	return out
}

// renderKnownWhen is what the model reads for a known meeting's timing: the
// resolved date when there is one, and the words actually said in brackets
// whenever any are on record — so the model can tell "بكرة" apart from a firm
// date without being handed the raw column.
func renderKnownWhen(m db.Meeting, loc *time.Location) string {
	when := "no date yet"
	if m.StartsAt > 0 {
		when = time.Unix(m.StartsAt, 0).In(loc).Format("2006-01-02 Monday 15:04")
	}
	var words []string
	if m.TimeOptions != "" {
		_ = json.Unmarshal([]byte(m.TimeOptions), &words)
	}
	if len(words) > 0 {
		when += " [" + strings.Join(words, ", ") + "]"
	}
	return when
}

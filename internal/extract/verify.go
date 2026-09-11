package extract

import (
	"strings"
	"unicode"
)

// Checking the model's homework.
//
// This is the half of the design that makes a local model usable. The model is
// allowed to be over-eager; nothing it says is trusted until the words it
// quoted are found in the message it named. A claim that cannot be located is
// not a weak task, it is a fabrication, and it is dropped without ceremony.

// RejectReason says why a proposal was dropped, so a precision problem is
// visible in the data rather than guessed at.
type RejectReason string

const (
	RejectNoEvidenceID RejectReason = "no_evidence_id" // named a message that is not here
	RejectContextOnly  RejectReason = "context_only"   // kept for old rows; no longer produced
	RejectBadQuote     RejectReason = "bad_quote"      // the words are not in that message
	RejectEmptyTitle   RejectReason = "empty_title"    //
	RejectQuestion     RejectReason = "question_only"  // asking is not assigning
	RejectFinished     RejectReason = "already_done"   // a report of work behind them
	RejectMeeting      RejectReason = "meeting"        // arranging a meeting, which has its own module
	RejectLowScore     RejectReason = "low_confidence" //
	RejectTooShort     RejectReason = "too_short"      // too few words to name work
)

// Verified is a proposal that survived checking, with the line it came from.
//
// chunkLines is kept so the owner resolver can look up a reply target without
// the chunk having to be passed around beside every proposal.
type Verified struct {
	Task       ProposedTask
	Line       Line
	chunkLines []Line
}

// MinConfidence is the floor. The model is asked to spread its range; anything
// it is less than 40% sure of is not worth a human's attention.
const MinConfidence = 0.4

// Verify checks one proposal against the chunk it came from.
func Verify(c Chunk, p ProposedTask) (Verified, RejectReason, bool) {
	if strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 140 {
		return Verified{}, RejectEmptyTitle, false
	}
	if p.Confidence < MinConfidence {
		return Verified{}, RejectLowScore, false
	}

	id := strings.TrimPrefix(strings.TrimSpace(p.EvidenceID), "#")
	var line Line
	var found bool
	for _, l := range c.Lines {
		if l.MessageID == id {
			line, found = l, true
			break
		}
	}
	if !found {
		return Verified{}, RejectNoEvidenceID, false
	}
	// A context line used to be rejected here, on the reasoning that the chunk
	// which owns the line will produce the task anyway. Measured against
	// hand-labelled chats, that reasoning was wrong: it threw away fourteen
	// correct tasks in one run, because a request often only becomes obvious
	// once somebody answers it — and the answer lives in the NEXT chunk, where
	// the request is context. The duplicate this allows is handled where
	// duplicates belong, in dedupe.
	if !quoteMatches(line, p.Evidence) {
		// The words are not in the message the model named. Before calling it
		// a fabrication, look for the message they ARE in: models copy a quote
		// correctly and then cite the id of the line above or below it, and
		// four real tasks were thrown away for that in one measured run.
		//
		// Only an exact, unique hit counts. If two lines contain the quote, or
		// none does, there is nothing to anchor to and it is dropped.
		fixed, ok := lineHoldingQuote(c, p.Evidence, line.Sender)
		if !ok {
			return Verified{}, RejectBadQuote, false
		}
		line = fixed
	}
	if isMeetingArrangement(p.Title, p.Evidence) {
		// Meetings have their own module, with their own review queue, dates
		// and attendees. Letting them in here would mean the same conversation
		// produces a task AND a meeting, and the task would be the worse copy.
		return Verified{}, RejectMeeting, false
	}
	if isFinishedReport(p.Evidence) {
		// The work is behind them. A status update is not a task, and the
		// prompt cannot make a model stop offering them.
		return Verified{}, RejectFinished, false
	}
	if isQuestionOnly(p.Evidence) {
		// "ممكن مثال؟" is a question. The prompt says so and models still
		// return them, so the rule lives here where it is enforced rather than
		// requested.
		return Verified{}, RejectQuestion, false
	}
	// Last, because a short bare question is more usefully filed as a question
	// than as a fragment.
	if tooShortForWork(line.Text) {
		// "فيها مشكله" became "fix the problem in the chat", and "فل الفل"
		// ("great") became "check the system works". The quote matched both
		// times, because the words really were said — the model had simply
		// built a task out of an acknowledgement.
		//
		// The MESSAGE is what is measured, not the quote: quoting "جهز العقد"
		// out of "جهز العقد قبل الخميس" is a fair quote of a real request.
		return Verified{}, RejectTooShort, false
	}
	return Verified{Task: p, Line: line, chunkLines: c.Lines}, "", true
}

// quoteMatches asks whether the model's words really appear in the message.
//
// Exact containment after normalising whitespace and Arabic diacritics. For a
// transcript the bar is softer: models tidy up ASR output while quoting it, so
// a close match is accepted, and it is still anchored to a real message.
func quoteMatches(l Line, quote string) bool {
	q := normalise(stripRenderArtefacts(quote, l.Sender))
	if q == "" {
		return false
	}
	hay := normalise(l.Text)
	if strings.Contains(hay, q) {
		return true
	}
	// An exact copy is what is asked for, and mostly what arrives. But models
	// make small edits even to typed text — qwen2.5 quoted "الاكسز" where the
	// message said "اكسز", adding the definite article, and a genuine task was
	// thrown away for one letter.
	//
	// So a near miss is allowed, with the anchor intact: the words must still
	// be in THIS message, which is what stops a fabrication. Typed text is held
	// to a higher share than a transcript, because there is nothing to correct.
	if l.MediaType == "voice_note" || l.MediaType == "audio" {
		return wordOverlap(hay, q) >= 0.8
	}
	return wordOverlap(hay, q) >= 0.9
}

// stripRenderArtefacts removes the parts of a rendered line that belong to the
// format rather than to the message.
//
// Models copy what they see. Shown "[#3AE9…] [Mon 27 Jul] Fady Mondy: وما
// تنسوا…", qwen2.5 returned "@Fady Mondy وما تنسوا…" — the words were exact,
// but the speaker had been folded into the quote, and four correct tasks were
// thrown away as fabrications. Asking the prompt to stop helps; refusing to be
// fooled by it is what makes the check trustworthy.
func stripRenderArtefacts(quote, sender string) string {
	q := strings.TrimSpace(quote)

	// A leading "[#id]" copied from the line prefix.
	if strings.HasPrefix(q, "[#") {
		if i := strings.Index(q, "]"); i > 0 {
			q = strings.TrimSpace(q[i+1:])
		}
	}
	// A leading timestamp, same origin.
	if strings.HasPrefix(q, "[") {
		if i := strings.Index(q, "]"); i > 0 {
			q = strings.TrimSpace(q[i+1:])
		}
	}
	// The speaker, with or without the "@" the renderer uses for mentions.
	for _, prefix := range []string{"@" + sender, sender} {
		if prefix == "@" || prefix == "" {
			continue
		}
		if strings.HasPrefix(q, prefix) {
			q = strings.TrimSpace(strings.TrimPrefix(q, prefix))
			q = strings.TrimSpace(strings.TrimPrefix(q, ":"))
			break
		}
	}
	return q
}

// wordOverlap is the share of the quote's words that appear in the message.
//
// Single letters count as hits rather than misses: Arabic particles and the
// definite article are exactly what a model adds or drops while quoting, and
// punishing them would reject correct evidence over nothing.
func wordOverlap(hay, quote string) float64 {
	words := strings.Fields(quote)
	if len(words) == 0 {
		return 0
	}
	hit := 0
	for _, w := range words {
		if len([]rune(w)) < 2 || strings.Contains(hay, w) {
			hit++
			continue
		}
		// Try without a leading definite article, the commonest edit of all.
		if trimmed := strings.TrimPrefix(w, "ال"); trimmed != w && len([]rune(trimmed)) >= 2 {
			if strings.Contains(hay, trimmed) {
				hit++
			}
		}
	}
	return float64(hit) / float64(len(words))
}

// Past-tense reports of finished work.
//
// The prompt forbids them and models return them anyway: "عملت شوية تعديلات"
// — "I made some edits" — came back as a task on every run. These are the
// first-person past forms that actually appear in these chats, and they are
// distinctive enough to act on: a message that opens by reporting something
// done is a status update, whatever else it contains.
var pastReportWords = []string{
	"عملت", "خلصت", "انجزت", "أنجزت", "بعتت", "بعتها", "نقلت", "راجعت", "جهزت",
	"ارسلت", "أرسلت", "رفعت", "كتبت", "حدثت", "سويت", "اتفقنا", "التقيت", "قابلت",
	"عرضت", "اضفت", "أضفت", "نزلت", "عملنا", "اتفقت", "طلبت", "شرحت", "ركبت",
	"i did", "i sent", "i finished", "i completed", "i prepared", "i reviewed",
	"already sent", "already done",
}

// isFinishedReport reports whether the evidence is somebody saying the work is
// already behind them.
func isFinishedReport(evidence string) bool {
	e := normalise(evidence)
	// Only the opening counts. "لازم نعمل كذا زي ما عملت امبارح" is a request
	// that happens to mention the past; "عملت شوية تعديلات" is a report.
	head := e
	if r := []rune(e); len(r) > 60 {
		head = string(r[:60])
	}
	for _, w := range pastReportWords {
		if !strings.Contains(head, w) {
			continue
		}
		// A report that also asks for something is still a request. The past
		// word has to be cut out before looking, because several request stems
		// live inside their own past tense: "خلص" (finish it) sits inside
		// "خلصت" (I finished), and "راجع" inside "راجعت". Left in, every report
		// looked like a request and the rule never fired.
		rest := strings.ReplaceAll(e, w, " ")
		for _, rw := range requestWords {
			if strings.Contains(rest, rw) {
				return false
			}
		}
		return true
	}
	return false
}

// isQuestionOnly reports whether the evidence is nothing but a question.
//
// A question that also carries a request ("ممكن تبعتلي الملف؟") is a task; a
// bare "شو رايكم؟" is not. So a question mark alone does not disqualify —
// a question mark with no request verb does.
func isQuestionOnly(evidence string) bool {
	e := normalise(evidence)
	if !strings.ContainsAny(e, "?؟") {
		return false
	}
	for _, w := range requestWords {
		if strings.Contains(e, w) {
			return false
		}
	}
	// Short questions are the noisy ones: "ممكن مثال؟", "ليش؟", "any update?".
	// A long question usually carries its own request even without a keyword.
	return len([]rune(e)) < 80
}

// normalise makes two pieces of Arabic or English text comparable: collapsed
// whitespace, folded case, and stripped diacritics and tatweel, which people
// type inconsistently and models drop when quoting.
func normalise(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0x064B && r <= 0x0652: // Arabic diacritics
			continue
		case r == 0x0640: // tatweel
			continue
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Arranging a meeting, as opposed to doing work.
//
// The distinction is narrow but real: "جهز العرض قبل الاجتماع" is a task —
// prepare something, ahead of a meeting. "نتفق على موعد الاجتماع" is the
// meeting itself being arranged, and that belongs to the meetings module,
// which already tracks proposed times, attendees and agendas properly.
//
// Live on a real group, qwen2.5 produced two tasks in a row that were both
// "agree a time for the weekly meeting", despite the prompt forbidding it.
var meetingNouns = []string{
	"اجتماع", "الاجتماع", "اجتماعنا", "لقاء", "ميتنج", "meeting", "call",
	// A meeting goes by many names: a session, an appointment, a sit-down.
	// Measured, these four were the ones slipping through as tasks.
	"جلسة", "الجلسة", "جلستنا", "موعد", "الموعد", "موعدنا", "قعدة", "session",
}

var arrangeVerbs = []string{
	"تحديد", "نحدد", "حدد", "اقتراح", "اقترح", "نتفق", "تثبيت", "نثبت", "ترتيب",
	"نرتب", "جدولة", "موعد", "مواعيد", "اجراء", "إجراء", "عقد", "حضور",
	"schedule", "arrange", "set up", "set a time", "agree a time", "propose",
	"book", "hold a", "attend",
}

func isMeetingArrangement(title, evidence string) bool {
	t := normalise(title)
	hasMeeting := false
	for _, n := range meetingNouns {
		if strings.Contains(t, n) {
			hasMeeting = true
			break
		}
	}
	if !hasMeeting {
		return false
	}
	// The title has to be ABOUT arranging it. "prepare the deck for the
	// meeting" mentions a meeting and is still a task.
	for _, v := range arrangeVerbs {
		if strings.Contains(t, v) {
			return true
		}
	}
	return false
}

// TitleSimilarity says how much two titles say the same thing, from 0 to 1.
//
// The evaluation command uses it to match what a model proposed against what a
// person labelled. It lives here so both sides read Arabic the same way — the
// same diacritics stripped, the same definite article ignored.
func TitleSimilarity(a, b string) float64 {
	wa, wb := titleWords(a), titleWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	// Against the shorter title, so "جهز العقد" still matches
	// "جهز العقد قبل الخميس".
	shorter := len(wa)
	if len(wb) < shorter {
		shorter = len(wb)
	}
	return float64(shared) / float64(shorter)
}

func titleWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(normalise(s)) {
		w = strings.Trim(w, ".,!?:;\u060c\u061f\u2026()[]\"'")
		w = strings.TrimPrefix(w, "\u0627\u0644") // the definite article
		if len([]rune(w)) >= 2 {
			out[w] = true
		}
	}
	return out
}

// minWorkWords and minWorkRunes are the floor for a quote that names work.
//
// Set from the shortest real requests there are: "ممكن تبعتلي الملف؟" is three
// words and eighteen characters, and it is as plain an ask as exists. Below
// that is an acknowledgement, a greeting or a fragment — "فيها مشكله",
// "فل الفل", "سلم عليه", "اذا كنت في جدة".
const (
	minWorkWords = 3
	minWorkRunes = 15
)

func tooShortForWork(evidence string) bool {
	e := strings.TrimSpace(normalise(evidence))
	return len(strings.Fields(e)) < minWorkWords || len([]rune(e)) < minWorkRunes
}

// lineHoldingQuote finds the one message in the chunk that really contains the
// quote, for when the model copied the words right and the id wrong.
func lineHoldingQuote(c Chunk, quote, sender string) (Line, bool) {
	q := normalise(stripRenderArtefacts(quote, sender))
	// Short quotes match too many lines to identify anything.
	if len([]rune(q)) < 25 {
		return Line{}, false
	}
	var hit Line
	found := 0
	for _, l := range c.Lines {
		if strings.Contains(normalise(l.Text), q) {
			hit, found = l, found+1
			if found > 1 {
				return Line{}, false
			}
		}
	}
	return hit, found == 1
}

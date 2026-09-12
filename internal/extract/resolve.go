package extract

import (
	"strconv"
	"strings"
	"time"
)

// Turning the model's hints into facts.
//
// Owners and dates are the two things every model tested got wrong, and both
// have a right answer sitting in the data. A message that says "@Sara ابعتلي
// الملف" names its owner exactly; a message sent on a Monday that says "الأحد"
// means the coming Sunday and nothing else. Neither is a matter of opinion, so
// neither is left to the model.

// OwnerSource records how an owner was found, which is worth keeping: it is
// the difference between a resolution to trust and one to review.
type OwnerSource string

const (
	OwnerFromMention OwnerSource = "mention"
	OwnerFromReply   OwnerSource = "reply"
	OwnerFromName    OwnerSource = "name"
	OwnerFromKunya   OwnerSource = "kunya"
	OwnerFromSelf    OwnerSource = "self"
	OwnerUnknown     OwnerSource = "none"
)

// ResolveOwner decides who has to do the work.
//
// The order is by reliability, not by convenience. A mention is a fact. A
// name typed by the model is a guess that happens to be checkable. Anything
// unresolved stays empty and goes to review with no owner, which is honest —
// an invented owner is worse than a missing one, because nobody double-checks
// a field that looks filled in.
func ResolveOwner(v Verified, roster []RosterPerson) (jid string, src OwnerSource) {
	line := v.Line
	text := normalise(line.Text)

	// 1. Somebody was @-mentioned in the very message that asks for the work.
	if len(line.Mentions) == 1 {
		return line.Mentions[0], OwnerFromMention
	}
	if len(line.Mentions) > 1 {
		// Several people mentioned: only useful if the model named one of them.
		if p := matchRoster(v.Task.OwnerText, roster); p != nil {
			for _, m := range line.Mentions {
				if m == p.JID {
					return p.JID, OwnerFromMention
				}
			}
		}
	}

	// 2. A request written as a reply is aimed at whoever it answers — unless
	// it answers YOUR OWN message. People reply to themselves constantly, to
	// add a thought to what they just said, and reading that as "you own this"
	// made the person asking for the work the person who must do it.
	if line.ReplyTo != "" && looksLikeRequest(text) {
		if jid := replyTargetJID(v, line.ReplyTo); jid != "" && !sameParty(jid, line.SenderJID) {
			return jid, OwnerFromReply
		}
	}

	// 3 & 4. The model named somebody. Believe it only if the roster agrees.
	if p := matchRoster(v.Task.OwnerText, roster); p != nil {
		if strings.EqualFold(strings.TrimSpace(v.Task.OwnerText), p.Kunya) {
			return p.JID, OwnerFromKunya
		}
		return p.JID, OwnerFromName
	}

	// 5. Somebody volunteering owns it themselves.
	if looksLikeCommitment(text) && line.SenderJID != "" {
		return line.SenderJID, OwnerFromSelf
	}

	return "", OwnerUnknown
}

func replyTargetJID(v Verified, replyTo string) string {
	// The chunk carries the replied-to line whenever it exists, because
	// chunking guarantees it.
	for _, l := range v.chunkLines {
		if l.MessageID == replyTo {
			return l.SenderJID
		}
	}
	return ""
}

// matchRoster finds the person the model meant. Exact name first, then kunya,
// then a shared distinctive word — "Sara" should match "Sara Haddad", but two
// letters must never match anything.
func matchRoster(ownerText string, roster []RosterPerson) *RosterPerson {
	want := normalise(ownerText)
	if want == "" || want == "unknown" {
		return nil
	}
	for i := range roster {
		if normalise(roster[i].Name) == want || normalise(roster[i].Kunya) == want {
			return &roster[i]
		}
	}
	for i := range roster {
		for _, part := range strings.Fields(normalise(roster[i].Name) + " " + normalise(roster[i].Kunya)) {
			if len([]rune(part)) < 3 {
				continue
			}
			for _, w := range strings.Fields(want) {
				if w == part {
					return &roster[i]
				}
			}
		}
	}
	return nil
}

// Volunteering, in the forms people actually use.
//
// "علي" was in this list and had to come out: it is three letters that sit
// inside "عليه", "عليك" and the name Ali, so a message saying "نشتغل عليه"
// was read as somebody taking the job. A commitment phrase has to be long
// enough to mean only one thing.
var commitmentWords = []string{
	"انا هعملها", "انا بعملها", "بعملها", "هعملها", "هعمله", "بجهزها", "بجهزه",
	"هبعتلك", "بعتلك", "برسلها", "هرسلها", "انا عليها", "i'll", "i will",
	"on it", "will do",
}

func looksLikeRequest(text string) bool {
	for _, w := range requestWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func looksLikeCommitment(text string) bool {
	for _, w := range commitmentWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// ResolveDue turns the words people actually use into a timestamp.
//
// This is code and not the model because the model gets it wrong in a way
// nobody notices: asked on Monday 7 September what date "الأحد" is, it
// answered Monday the 14th. The message's own timestamp settles it.
//
// Returns 0 when the phrase means nothing certain. An absent due date is fine;
// a confidently wrong one is not.
func ResolveDue(dueText string, sentAt int64, loc *time.Location) int64 {
	t := normalise(dueText)
	if t == "" || sentAt == 0 {
		return 0
	}
	if loc == nil {
		loc = time.UTC
	}
	sent := time.Unix(sentAt, 0).In(loc)
	day := func(d time.Time) int64 {
		// End of the working day, rather than midnight, which reads as the day
		// before in every list.
		return time.Date(d.Year(), d.Month(), d.Day(), 18, 0, 0, 0, loc).Unix()
	}

	switch {
	case containsAny(t, "اليوم", "today", "النهاردة"):
		return day(sent)
	case containsAny(t, "بكرة", "بكره", "غدا", "غدآ", "tomorrow"):
		return day(sent.AddDate(0, 0, 1))
	case containsAny(t, "بعد بكرة", "بعد بكره", "day after tomorrow"):
		return day(sent.AddDate(0, 0, 2))
	}

	// "in 3 days" / "خلال 3 ايام"
	if n, ok := numberBefore(t, "days", "ايام", "أيام", "يوم"); ok {
		return day(sent.AddDate(0, 0, n))
	}
	if n, ok := numberBefore(t, "weeks", "اسابيع", "أسابيع", "اسبوع"); ok {
		return day(sent.AddDate(0, 0, 7*n))
	}

	if wd, ok := weekday(t); ok {
		// The next occurrence, never today: "الخميس" said on a Thursday means
		// the Thursday coming, not the one that is nearly over.
		diff := (int(wd) - int(sent.Weekday()) + 7) % 7
		if diff == 0 {
			diff = 7
		}
		return day(sent.AddDate(0, 0, diff))
	}

	switch {
	case containsAny(t, "نهاية الاسبوع", "نهاية الأسبوع", "end of week", "end of the week"):
		// The working week here ends on Thursday.
		diff := (int(time.Thursday) - int(sent.Weekday()) + 7) % 7
		return day(sent.AddDate(0, 0, diff))
	case containsAny(t, "نهاية الشهر", "end of month", "end of the month"):
		first := time.Date(sent.Year(), sent.Month(), 1, 0, 0, 0, 0, loc)
		return day(first.AddDate(0, 1, -1))
	case containsAny(t, "الاسبوع الجاي", "الأسبوع الجاي", "الاسبوع القادم", "next week"):
		return day(sent.AddDate(0, 0, 7))
	}

	// Anything else — "قريب", "بعد السفر", "soon" — is a real phrase but not a
	// date. The words are kept on the task; the field stays empty.
	return 0
}

func containsAny(hay string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

var weekdayWords = map[string]time.Weekday{
	"الأحد": time.Sunday, "الاحد": time.Sunday, "sunday": time.Sunday,
	"الاثنين": time.Monday, "الإثنين": time.Monday, "monday": time.Monday,
	"الثلاثاء": time.Tuesday, "tuesday": time.Tuesday,
	"الأربعاء": time.Wednesday, "الاربعاء": time.Wednesday, "wednesday": time.Wednesday,
	"الخميس": time.Thursday, "thursday": time.Thursday,
	"الجمعة": time.Friday, "friday": time.Friday,
	"السبت": time.Saturday, "saturday": time.Saturday,
}

func weekday(t string) (time.Weekday, bool) {
	for word, wd := range weekdayWords {
		if strings.Contains(t, normalise(word)) {
			return wd, true
		}
	}
	return 0, false
}

// numberBefore finds "3 days" or "خلال ٣ ايام", including Arabic-Indic digits.
func numberBefore(t string, units ...string) (int, bool) {
	for _, u := range units {
		i := strings.Index(t, u)
		if i < 0 {
			continue
		}
		fields := strings.Fields(t[:i])
		for j := len(fields) - 1; j >= 0 && j >= len(fields)-2; j-- {
			if n, err := strconv.Atoi(westernDigits(fields[j])); err == nil && n > 0 && n < 365 {
				return n, true
			}
		}
	}
	return 0, false
}

func westernDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '٠' && r <= '٩': // Arabic-Indic
			b.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹': // Extended Arabic-Indic
			b.WriteRune('0' + (r - '۰'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameParty compares two identities by their digits, so a LID and a phone
// number for one person do not read as two people.
func sameParty(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return partyDigits(a) == partyDigits(b)
}

func partyDigits(jid string) string {
	if i := strings.IndexAny(jid, "@:"); i >= 0 {
		jid = jid[:i]
	}
	return jid
}

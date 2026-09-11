package extract

import (
	"strings"
	"time"
)

// Cutting a conversation into pieces a model can hold at once.
//
// The rule that matters: a reply must never be separated from what it answers.
// "تمام" on its own is noise; "تمام" under "جهز العقد" is a commitment. A chunk
// boundary that loses that context does not lose a little accuracy, it changes
// the meaning.

type ChunkOptions struct {
	MaxMessages int
	MaxChars    int
	// Overlap repeats the tail of a chunk at the head of the next one, as
	// context, so a thread that straddles a boundary is still readable.
	Overlap int
	Loc     *time.Location
}

func DefaultChunkOptions(loc *time.Location) ChunkOptions {
	return ChunkOptions{MaxMessages: 180, MaxChars: 14000, Overlap: 10, Loc: loc}
}

// Split cuts lines into model-sized chunks.
//
// Boundaries prefer a change of day, because a day break is usually also a
// change of subject. Anything a later line replies to is carried forward as
// context, and context lines can be read but never extracted from.
func Split(lines []Line, chatJID string, opt ChunkOptions) []Chunk {
	if opt.MaxMessages <= 0 {
		opt.MaxMessages = 180
	}
	if opt.MaxChars <= 0 {
		opt.MaxChars = 14000
	}
	if opt.Loc == nil {
		opt.Loc = time.UTC
	}
	if len(lines) == 0 {
		return nil
	}

	byID := make(map[string]Line, len(lines))
	for _, l := range lines {
		byID[l.MessageID] = l
	}

	var out []Chunk
	start := 0
	for start < len(lines) {
		end := start
		chars := 0
		for end < len(lines) {
			next := chars + len(lines[end].Text) + 60 // 60 ≈ the rendered prefix
			atLimit := (end-start) >= opt.MaxMessages || next > opt.MaxChars
			if atLimit && end > start {
				// Prefer to break where the day changes, if one is close behind.
				if b := dayBreakBefore(lines, start, end, opt.Loc); b > start {
					end = b
				}
				break
			}
			chars = next
			end++
		}
		if end == start {
			end = start + 1 // a single enormous message still has to go somewhere
		}

		body := lines[start:end]
		chunk := Chunk{ChatJID: chatJID, Index: len(out)}

		// Carry in the tail of the previous chunk, and anything this chunk
		// replies to that sits outside it.
		var head []Line
		if start > 0 && opt.Overlap > 0 {
			from := start - opt.Overlap
			if from < 0 {
				from = 0
			}
			head = append(head, asContext(lines[from:start])...)
		}
		inChunk := map[string]bool{}
		for _, l := range body {
			inChunk[l.MessageID] = true
		}
		for _, l := range head {
			inChunk[l.MessageID] = true
		}
		for _, l := range body {
			if l.ReplyTo == "" || inChunk[l.ReplyTo] {
				continue
			}
			if target, ok := byID[l.ReplyTo]; ok {
				c := target
				c.Context = true
				head = append(head, c)
				inChunk[c.MessageID] = true
			}
		}

		chunk.Lines = append(append([]Line{}, head...), body...)
		chunk.Rendered = Render(chunk, opt.Loc)
		out = append(out, chunk)
		start = end
	}
	return out
}

func asContext(in []Line) []Line {
	out := make([]Line, 0, len(in))
	for _, l := range in {
		l.Context = true
		out = append(out, l)
	}
	return out
}

// dayBreakBefore finds the last index in (start, end) where the local day
// changes, so a chunk can end on a natural seam. Returns start when there is
// none close enough to be worth it.
func dayBreakBefore(lines []Line, start, end int, loc *time.Location) int {
	best := start
	for i := start + 1; i < end; i++ {
		if time.Unix(lines[i].TS, 0).In(loc).YearDay() !=
			time.Unix(lines[i-1].TS, 0).In(loc).YearDay() {
			best = i
		}
	}
	// Only take the seam if it keeps most of the chunk; otherwise the chunks
	// get tiny and the model loses the thread.
	if best-start < (end-start)/2 {
		return start
	}
	return best
}

// Actionable signal, used to skip chunks that cannot contain work.
//
// This is an optimisation, never a filter. A chunk is skipped only when every
// single line scores zero, and a skip is logged — a missed task is worse than
// a wasted model call.

var requestWords = []string{
	// Arabic, as people actually type it.
	//
	// Several of these are deliberately stems rather than whole words. Arabic
	// verbs take a prefix for person — ابعتلي (I ask you to send), تبعتلي (you
	// send me), يبعتلي (he sends me) — so matching "بعتل" catches the request
	// however it was conjugated. Matching only "ابعتلي" missed "ممكن تبعتلي
	// الملف؟", which is as plain a request as they come.
	"بعتل", "ابعت", "إبعت", "جهز", "جهّز", "راجع", "لازم", "بدي منك", "بدنا",
	"ياريت", "رجاء", "من فضلك", "تابع", "ذكرني", "حدد", "اعمل", "سوي", "كمل",
	"ارسل", "أرسل", "شوف", "اتأكد", "تأكد", "خلص", "مطلوب", "كلف", "سلم",
	// English
	"please", "can you", "could you", "need to", "make sure", "send me",
	"follow up", "prepare", "review", "deadline", "asap", "assign", "by eod",
}

var deadlineWords = []string{
	"بكرة", "بكره", "غدا", "غداً", "اليوم", "الأحد", "الاحد", "الاثنين", "الإثنين",
	"الثلاثاء", "الأربعاء", "الاربعاء", "الخميس", "الجمعة", "السبت", "نهاية الأسبوع",
	"نهاية الشهر", "الأسبوع الجاي", "tomorrow", "today", "monday", "tuesday",
	"wednesday", "thursday", "friday", "saturday", "sunday", "next week",
	"end of week", "end of month",
}

// Signal scores one line for "could this contain work".
func Signal(l Line, taskOrigins map[string]bool) int {
	if l.Context {
		return 0
	}
	score := 0
	low := strings.ToLower(l.Text)
	for _, w := range requestWords {
		if strings.Contains(low, w) {
			score += 2
			break
		}
	}
	for _, w := range deadlineWords {
		if strings.Contains(low, w) {
			score++
			break
		}
	}
	if len(l.Mentions) > 0 {
		score++
	}
	if strings.ContainsAny(l.Text, "?؟") {
		score++
	}
	// A reply to a known task's origin is how completions arrive.
	if l.ReplyTo != "" && taskOrigins[l.ReplyTo] {
		score += 2
	}
	return score
}

// Skippable is true only when nothing in the chunk could possibly be work.
func Skippable(c Chunk, taskOrigins map[string]bool) bool {
	for _, l := range c.Lines {
		if Signal(l, taskOrigins) > 0 {
			return false
		}
	}
	return true
}

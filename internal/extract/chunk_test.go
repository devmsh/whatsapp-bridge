package extract_test

import (
	"strings"
	"testing"

	"whatsapp-bridge-v2/internal/extract"
)

func line(id string, ts int64, sender, text string) extract.Line {
	return extract.Line{MessageID: id, TS: ts, Sender: sender, Text: text}
}

// TestChunkKeepsRepliesWithTheirTarget is the rule the whole design rests on.
// "تمام" alone is noise. "تمام" under "جهز العقد" is a commitment. A boundary
// that separates them changes what the text means.
func TestChunkKeepsRepliesWithTheirTarget(t *testing.T) {
	var lines []extract.Line
	for i := 0; i < 12; i++ {
		lines = append(lines, line(string(rune('a'+i)), int64(1000+i), "Sami", "filler filler"))
	}
	// The last line answers the very first one, which will fall outside.
	last := line("z", 2000, "Sara", "تمام")
	last.ReplyTo = "a"
	lines = append(lines, last)

	chunks := extract.Split(lines, "c@g.us",
		extract.ChunkOptions{MaxMessages: 6, MaxChars: 100000, Overlap: 0})
	if len(chunks) < 2 {
		t.Fatalf("expected the lines to split, got %d chunk(s)", len(chunks))
	}

	final := chunks[len(chunks)-1]
	var sawTarget, targetIsContext bool
	for _, l := range final.Lines {
		if l.MessageID == "a" {
			sawTarget, targetIsContext = true, l.Context
		}
	}
	if !sawTarget {
		t.Fatalf("the replied-to message must be carried into the chunk")
	}
	if !targetIsContext {
		t.Errorf("a carried-in message is context, not something to extract from")
	}
}

// TestChunkOverlapIsContext checks the tail of one chunk reappears in the next
// as readable-but-not-extractable.
func TestChunkOverlapIsContext(t *testing.T) {
	var lines []extract.Line
	for i := 0; i < 10; i++ {
		lines = append(lines, line(string(rune('a'+i)), int64(1000+i), "Sami", "x"))
	}
	chunks := extract.Split(lines, "c@g.us",
		extract.ChunkOptions{MaxMessages: 5, MaxChars: 100000, Overlap: 2})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	head := chunks[1].Lines[0]
	if !head.Context {
		t.Errorf("overlapped lines must be marked as context")
	}
	if !strings.Contains(chunks[1].Rendered, "[context]") {
		t.Errorf("rendered chunk should show the context marker")
	}
}

// TestSkippableOnlyWhenNothingCouldBeWork guards the recall side of the
// prefilter. Skipping is an optimisation; skipping something real is a bug.
func TestSkippableOnlyWhenNothingCouldBeWork(t *testing.T) {
	quiet := extract.Chunk{Lines: []extract.Line{
		line("1", 1, "Sami", "صباح الخير"),
		line("2", 2, "Sara", "الله يصبحك بالخير"),
		line("3", 3, "Sami", "😂😂"),
	}}
	if !extract.Skippable(quiet, nil) {
		t.Errorf("a chunk of greetings has nothing to extract")
	}

	for _, text := range []string{
		"ابعتلي الملف",
		"جهز العقد قبل الخميس",
		"please review the contract",
		"can you follow up with them?",
	} {
		busy := extract.Chunk{Lines: []extract.Line{
			line("1", 1, "Sami", "صباح الخير"),
			line("2", 2, "Sara", text),
		}}
		if extract.Skippable(busy, nil) {
			t.Errorf("a chunk containing %q must not be skipped", text)
		}
	}
}

// TestSignalNoticesCompletionReplies — a reply to a task's origin is how
// "done" arrives, so it has to keep the chunk alive even if nothing else does.
func TestSignalNoticesCompletionReplies(t *testing.T) {
	l := line("2", 2, "Sara", "تم")
	l.ReplyTo = "origin-1"
	c := extract.Chunk{Lines: []extract.Line{l}}
	if extract.Skippable(c, map[string]bool{"origin-1": true}) {
		t.Errorf("a reply to a known task origin must keep the chunk")
	}
	if !extract.Skippable(c, nil) {
		t.Errorf("without that context the same line is just chatter")
	}
}

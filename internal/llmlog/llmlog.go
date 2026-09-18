// Package llmlog records every call made to a model.
//
// Why it is its own package: the adapters that call models are not allowed to
// open the database (see internal/extract/port.go — that rule is what keeps a
// model swap to one file). So the adapters write to this package instead, and
// the API layer plugs the database in behind it at startup.
//
// Until something plugs a sink in, recording is a no-op. That is deliberate:
// tests and the eval command run the same adapters and should not need a
// database to do it.
package llmlog

import (
	"strings"
	"sync"
	"time"
)

// MaxText is the cap on a stored prompt or answer, in bytes.
//
// 64KB holds a full 16k-token chunk with room to spare, and stops one runaway
// call from putting a megabyte in the database. Anything longer is cut and
// marked, so a truncated record still says it was truncated.
const MaxText = 64 * 1024

// Call is one request to a model and what came back.
type Call struct {
	RunID   string
	Service string // tasks | meetings | media | profiles
	Engine  string // ollama | claude | codex
	Model   string
	Kind    string // extract | judge | completion | meetings | describe | refine
	ChatJID string

	System   string
	Prompt   string
	Response string

	Latency time.Duration
	// Attempt is 1 for the first try. A retry after unusable JSON is its own
	// record, because "it worked on the second go" is the interesting part.
	Attempt int
	Err     error
}

// Sink is whatever stores the calls. The database implements it.
type Sink interface {
	RecordLLMCall(c Call)
}

var (
	mu   sync.RWMutex
	sink Sink
)

// SetSink installs the store. Called once at startup. Passing nil turns
// recording back off.
func SetSink(s Sink) {
	mu.Lock()
	sink = s
	mu.Unlock()
}

// Enabled says whether anything is listening, so a caller can skip building a
// record nobody will keep.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return sink != nil
}

// Record stores one call. Safe to call with no sink installed.
//
// It never blocks the caller for long and never panics: a debugging log that
// can break extraction is worse than no log.
func Record(c Call) {
	mu.RLock()
	s := sink
	mu.RUnlock()
	if s == nil {
		return
	}
	c.System = Truncate(c.System)
	c.Prompt = Truncate(c.Prompt)
	c.Response = Truncate(c.Response)
	if c.Attempt < 1 {
		c.Attempt = 1
	}
	defer func() { _ = recover() }()
	s.RecordLLMCall(c)
}

// Truncate cuts text to MaxText and says so. Cutting on a rune boundary keeps
// Arabic readable instead of ending in half a letter.
func Truncate(s string) string {
	if len(s) <= MaxText {
		return s
	}
	cut := s[:MaxText]
	for len(cut) > 0 && !isRuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, "\x00") + "\n… [cut: " + itoa(len(s)) + " bytes in total]"
}

// A UTF-8 continuation byte is 10xxxxxx; anything else starts a rune.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

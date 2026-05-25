package wa

import (
	"sync"
	"time"
)

// typingFreshSec — how long after the last 'composing' beacon we keep
// reporting a participant as typing. WhatsApp keeps re-sending the beacon
// every few seconds while the user is at the keyboard and emits 'paused'
// the moment they stop, so 10s comfortably covers a single keystroke gap
// while still aging out anyone whose 'paused' got lost.
const typingFreshSec = 10

// typingState holds the per-chat set of senders currently composing. Used
// for the group "X is typing…" header — DM 1:1 typing already goes through
// the presence_cache table.
//
// Stored in-memory only: typing is purely ephemeral and would be misleading
// across restarts anyway.
type typingState struct {
	mu sync.Mutex
	// by chat JID -> by sender JID -> Unix seconds of last 'composing' beacon
	m map[string]map[string]int64
}

func newTypingState() *typingState {
	return &typingState{m: map[string]map[string]int64{}}
}

// Set marks `sender` as typing in `chatJID` now. Replaces any previous beacon
// from that sender — beacons arrive every few seconds while typing continues.
func (t *typingState) Set(chatJID, sender string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	inner, ok := t.m[chatJID]
	if !ok {
		inner = map[string]int64{}
		t.m[chatJID] = inner
	}
	inner[sender] = time.Now().Unix()
}

// Clear removes `sender` from the typing set for `chatJID` — called on the
// matching 'paused' beacon. Idempotent; safe even if nothing was set.
func (t *typingState) Clear(chatJID, sender string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	inner, ok := t.m[chatJID]
	if !ok {
		return
	}
	delete(inner, sender)
	if len(inner) == 0 {
		delete(t.m, chatJID)
	}
}

// Typers returns the senders currently typing in chatJID — beacons fresher
// than typingFreshSec. Stale entries are GC'd inline so the map can't grow
// unbounded across long-running sessions.
func (t *typingState) Typers(chatJID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	inner, ok := t.m[chatJID]
	if !ok {
		return nil
	}
	cutoff := time.Now().Unix() - typingFreshSec
	out := make([]string, 0, len(inner))
	for sender, ts := range inner {
		if ts >= cutoff {
			out = append(out, sender)
		} else {
			delete(inner, sender)
		}
	}
	if len(inner) == 0 {
		delete(t.m, chatJID)
	}
	return out
}

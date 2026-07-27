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
// typer is one live 'composing' beacon.
type typer struct {
	at int64 // Unix seconds of the last beacon
	// audio is true when the peer is recording a voice note rather than
	// typing text. WhatsApp shows these as different states, so we keep the
	// distinction rather than collapsing both to "typing…".
	audio bool
}

type typingState struct {
	mu sync.Mutex
	// by chat JID -> by sender JID -> latest beacon
	m map[string]map[string]typer
}

func newTypingState() *typingState {
	return &typingState{m: map[string]map[string]typer{}}
}

// Set marks `sender` as typing in `chatJID` now. Replaces any previous beacon
// from that sender — beacons arrive every few seconds while typing continues.
func (t *typingState) Set(chatJID, sender string, audio bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	inner, ok := t.m[chatJID]
	if !ok {
		inner = map[string]typer{}
		t.m[chatJID] = inner
	}
	inner[sender] = typer{at: time.Now().Unix(), audio: audio}
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

// Snapshot returns the full set of chats with at least one fresh typer:
//   chatJID -> []senderJID
// Stale entries (older than typingFreshSec) are GC'd inline, same as Typers.
// Used by the chat-list "typing…" preview — the client polls this once per
// tick instead of per-row, so N visible chats cost one request.
func (t *typingState) Snapshot() map[string][]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshot(false)
}

// SnapshotAudio is Snapshot restricted to senders recording a voice note.
// The client uses it to render "recording audio…" instead of "typing…".
func (t *typingState) SnapshotAudio() map[string][]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshot(true)
}

// snapshot must be called with the lock held. audioOnly filters to voice-note
// beacons; either way stale entries are GC'd inline.
func (t *typingState) snapshot(audioOnly bool) map[string][]string {
	cutoff := time.Now().Unix() - typingFreshSec
	out := make(map[string][]string, len(t.m))
	for chat, inner := range t.m {
		live := make([]string, 0, len(inner))
		for sender, e := range inner {
			if e.at < cutoff {
				delete(inner, sender)
				continue
			}
			if audioOnly && !e.audio {
				continue
			}
			live = append(live, sender)
		}
		if len(inner) == 0 {
			delete(t.m, chat)
			continue
		}
		if len(live) > 0 {
			out[chat] = live
		}
	}
	return out
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
	for sender, e := range inner {
		if e.at >= cutoff {
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

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// RunStatus is the lifecycle state of one extraction run.
type RunStatus string

const (
	RunStarting  RunStatus = "starting"
	RunRunning   RunStatus = "running"
	RunDone      RunStatus = "done"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// RunEvent is one line of progress for a run.
type RunEvent struct {
	TS    int64  `json:"ts"`
	Seq   int64  `json:"seq"`
	Kind  string `json:"kind"`           // tool | text | info | result | error
	Name  string `json:"name,omitempty"` // tool name (without mcp__whatsapp__ prefix)
	Text  string `json:"text,omitempty"` // free text or summary
}

// Run is the live state of one extraction.
type Run struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`               // chat | circle
	Subject   string     `json:"subject"`            // chat JID or circle id (string)
	Label     string     `json:"label"`              // human title
	Status    RunStatus  `json:"status"`
	StartedAt int64      `json:"started_at"`
	EndedAt   int64      `json:"ended_at,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	Created   int        `json:"created,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Error     string     `json:"error,omitempty"`
	Events    []RunEvent `json:"events,omitempty"`

	mu     sync.Mutex
	seq    int64
	subs   map[chan RunEvent]bool
	cancel context.CancelFunc
}

const runEventsCap = 2000 // ring-buffer of recent events kept in memory

// RunManager tracks live and recent extraction runs.
type RunManager struct {
	mu   sync.Mutex
	runs map[string]*Run
}

func newRunManager() *RunManager { return &RunManager{runs: map[string]*Run{}} }

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start creates a new Run with its own cancellable context.
func (m *RunManager) Start(kind, subject, label string) (*Run, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Run{
		ID:        newID(),
		Kind:      kind,
		Subject:   subject,
		Label:     label,
		Status:    RunStarting,
		StartedAt: time.Now().Unix(),
		subs:      map[chan RunEvent]bool{},
		cancel:    cancel,
	}
	m.mu.Lock()
	m.runs[r.ID] = r
	m.mu.Unlock()
	// Auto-clean finished runs after 30 minutes to keep the map small.
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for range t.C {
			r.mu.Lock()
			done := r.Status == RunDone || r.Status == RunFailed || r.Status == RunCancelled
			old := r.EndedAt > 0 && time.Now().Unix()-r.EndedAt > 30*60
			r.mu.Unlock()
			if done && old {
				m.mu.Lock()
				delete(m.runs, r.ID)
				m.mu.Unlock()
				return
			}
		}
	}()
	return r, ctx
}

func (m *RunManager) Get(id string) *Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[id]
}

func (m *RunManager) Active() []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Run{}
	for _, r := range m.runs {
		r.mu.Lock()
		live := r.Status == RunStarting || r.Status == RunRunning
		r.mu.Unlock()
		if live {
			out = append(out, r)
		}
	}
	return out
}

// AddEvent records a new event and fans it out to live subscribers.
func (r *Run) AddEvent(e RunEvent) {
	r.mu.Lock()
	r.seq++
	e.Seq = r.seq
	e.TS = time.Now().UnixMilli()
	r.Events = append(r.Events, e)
	if len(r.Events) > runEventsCap {
		// drop the oldest quarter at once to amortize copies
		r.Events = append([]RunEvent{}, r.Events[runEventsCap/4:]...)
	}
	subs := make([]chan RunEvent, 0, len(r.subs))
	for c := range r.subs {
		subs = append(subs, c)
	}
	r.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- e:
		default:
			// subscriber is slow — drop event for it rather than block all
		}
	}
}

// Subscribe returns a channel of new events plus an unsubscribe func.
// The caller should drain the channel.
func (r *Run) Subscribe() (chan RunEvent, func()) {
	c := make(chan RunEvent, 64)
	r.mu.Lock()
	r.subs[c] = true
	r.mu.Unlock()
	return c, func() {
		r.mu.Lock()
		delete(r.subs, c)
		r.mu.Unlock()
		close(c)
	}
}

// SetRunning marks the run as actively executing.
func (r *Run) SetRunning() {
	r.mu.Lock()
	r.Status = RunRunning
	r.mu.Unlock()
}

// Finish records the terminal state and final summary; closes all subscriptions.
func (r *Run) Finish(status RunStatus, sessionID, summary string, created int, errMsg string) {
	r.mu.Lock()
	r.Status = status
	r.EndedAt = time.Now().Unix()
	r.SessionID = sessionID
	r.Summary = summary
	r.Created = created
	r.Error = errMsg
	subs := make([]chan RunEvent, 0, len(r.subs))
	for c := range r.subs {
		subs = append(subs, c)
		delete(r.subs, c)
	}
	r.mu.Unlock()
	// One final event before closing each sub channel.
	kind := "result"
	if status == RunFailed || status == RunCancelled {
		kind = "error"
	}
	final := RunEvent{Kind: kind, Text: summary, Name: string(status), TS: time.Now().UnixMilli()}
	for _, c := range subs {
		select {
		case c <- final:
		default:
		}
		close(c)
	}
}

// Cancel triggers the run's context cancellation (kills the sidecar).
func (r *Run) Cancel() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// parseProgressLine turns one line of sidecar stderr into a structured event.
// The sidecar prints "→ <toolname>" before each tool call and emits the agent's
// own text lines as plain text. Anything else is "info".
func parseProgressLine(line string) RunEvent {
	t := strings.TrimRight(line, "\r\n")
	if t == "" {
		return RunEvent{}
	}
	if strings.HasPrefix(t, "→ ") {
		name := strings.TrimPrefix(t, "→ ")
		// strip mcp__whatsapp__ prefix so the UI reads cleanly
		name = strings.TrimPrefix(name, "mcp__whatsapp__")
		return RunEvent{Kind: "tool", Name: name}
	}
	return RunEvent{Kind: "text", Text: t}
}

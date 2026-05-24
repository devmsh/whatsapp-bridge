package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// AutoExtractor periodically re-runs incremental circle-level extraction on
// circles that have had new messages since their last watermark. It reuses the
// same RunManager and sidecar as a manual extraction.
//
// Gated behind a sync_state toggle: nothing happens until the user enables it.
type AutoExtractor struct {
	s            *Server
	mu           sync.Mutex
	running      bool   // true while a sidecar is mid-run
	lastRunID    string // most recent run, for debugging
	lastTickedAt int64
}

const (
	autoExtractEnabledKey  = "auto_extract_enabled"
	autoExtractIntervalKey = "auto_extract_interval_hours"
	autoExtractDefaultHrs  = 8
)

func newAutoExtractor(s *Server) *AutoExtractor { return &AutoExtractor{s: s} }

func (a *AutoExtractor) enabled() bool {
	v, _, _ := a.s.store.GetSyncState(autoExtractEnabledKey)
	return v == "1"
}

func (a *AutoExtractor) intervalHours() int {
	v, _, _ := a.s.store.GetSyncState(autoExtractIntervalKey)
	if v == "" {
		return autoExtractDefaultHrs
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return autoExtractDefaultHrs
	}
	return n
}

// Start launches the scheduler goroutine. It ticks every 10 minutes; on each
// tick, if enabled and no run is in flight, it picks one due circle with new
// messages and starts an extraction.
func (a *AutoExtractor) Start() {
	go func() {
		time.Sleep(30 * time.Second) // let sync settle on boot
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		a.tick()
		for range t.C {
			a.tick()
		}
	}()
}

func (a *AutoExtractor) tick() {
	a.mu.Lock()
	a.lastTickedAt = time.Now().Unix()
	if a.running || !a.enabled() {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	circleID, name, ok := a.pickDueCircle()
	if !ok {
		return
	}

	// Mirror the manual handler: start an async run via RunManager.
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()

	run, ctx := a.s.runs.Start("circle", strconv.FormatInt(circleID, 10), name+" (auto)")
	a.lastRunID = run.ID
	fmt.Printf("auto-extract: circle %d (%s) run=%s\n", circleID, name, run.ID)

	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}()
		a.s.executeExtraction(ctx, run, 30*time.Minute, "extract-circle.mjs",
			strconv.FormatInt(circleID, 10), name)
	}()
}

// pickDueCircle returns the circle id + name whose extraction is "most due":
// it has chats with messages newer than their watermark, and the circle hasn't
// been extracted in the last interval (rough check via earliest watermark).
// Returns ok=false if nothing needs a run right now.
func (a *AutoExtractor) pickDueCircle() (int64, string, bool) {
	circles, err := a.s.store.ListCircles()
	if err != nil {
		return 0, "", false
	}
	intervalSec := int64(a.intervalHours()) * 3600
	nowU := time.Now().Unix()

	var best struct {
		id        int64
		name      string
		gap       int64 // seconds since last watermark (older = more due)
		hasNew    bool
	}
	for _, c := range circles {
		jids, _ := a.s.store.FlattenCircleChats(c.ID)
		if len(jids) == 0 {
			continue
		}
		// For each chat in the circle: find max(timestamp) and watermark.
		// If max(timestamp) > watermark for any chat, this circle has new messages.
		var minWatermark int64 = nowU
		hasNew := false
		for _, jid := range jids {
			var maxTS, wmTS int64
			a.s.store.DB.QueryRow(`SELECT COALESCE(MAX(timestamp),0) FROM messages WHERE chat_jid = ?`, jid).Scan(&maxTS)
			a.s.store.DB.QueryRow(`SELECT COALESCE(last_msg_ts,0) FROM chat_extraction_state WHERE chat_jid = ?`, jid).Scan(&wmTS)
			if maxTS > wmTS {
				hasNew = true
			}
			if wmTS > 0 && wmTS < minWatermark {
				minWatermark = wmTS
			}
		}
		if !hasNew {
			continue
		}
		// Older than interval?
		gap := nowU - minWatermark
		if gap < intervalSec {
			continue
		}
		if gap > best.gap {
			best.id, best.name, best.gap, best.hasNew = c.ID, c.Name, gap, true
		}
	}
	if !best.hasNew {
		return 0, "", false
	}
	return best.id, best.name, true
}

// status returns the auto-extractor's current state for the UI.
type autoStatus struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Running       bool   `json:"running"`
	LastRunID     string `json:"last_run_id,omitempty"`
	LastTickedAt  int64  `json:"last_ticked_at,omitempty"`
}

// handleAutoExtract returns/updates the auto-extractor status + toggle.
//   GET  /api/v2/extractions/auto         -> current status
//   POST /api/v2/extractions/auto         -> {enabled?, interval_hours?}
func (s *Server) handleAutoExtract(w http.ResponseWriter, r *http.Request) {
	if s.autoExtract == nil {
		jsonError(w, 503, "auto-extract not initialised")
		return
	}
	a := s.autoExtract
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		st := autoStatus{
			Enabled:       a.enabled(),
			IntervalHours: a.intervalHours(),
			Running:       a.running,
			LastRunID:     a.lastRunID,
			LastTickedAt:  a.lastTickedAt,
		}
		a.mu.Unlock()
		jsonOK(w, st)
	case http.MethodPost:
		var req struct {
			Enabled       *bool `json:"enabled,omitempty"`
			IntervalHours *int  `json:"interval_hours,omitempty"`
		}
		decodeJSON(r, &req)
		if req.Enabled != nil {
			val := "0"
			if *req.Enabled {
				val = "1"
			}
			s.store.PutSyncState(autoExtractEnabledKey, val)
			if *req.Enabled {
				go a.tick() // give the user immediate feedback
			}
		}
		if req.IntervalHours != nil && *req.IntervalHours >= 1 {
			s.store.PutSyncState(autoExtractIntervalKey, strconv.Itoa(*req.IntervalHours))
		}
		a.mu.Lock()
		st := autoStatus{
			Enabled:       a.enabled(),
			IntervalHours: a.intervalHours(),
			Running:       a.running,
			LastRunID:     a.lastRunID,
			LastTickedAt:  a.lastTickedAt,
		}
		a.mu.Unlock()
		jsonOK(w, st)
	default:
		methodNotAllowed(w)
	}
}

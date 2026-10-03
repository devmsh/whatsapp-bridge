package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/extract/adapters"
)

// The debugging endpoints: one place that answers "what is this thing doing".
//
// Everything here is read-only and derived from state that already exists. It
// adds no behaviour of its own — which is the point. A debug screen that
// changes what the system does cannot be trusted to describe it.

// serviceInfo describes one background worker for the screen.
type serviceInfo struct {
	Name string `json:"name"`
	// What it does, in one line, so the screen does not need its own copy.
	Does string `json:"does"`
	// Enabled is the user's switch; Running is whether it is working now.
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Health  string `json:"health"` // ok | idle | off | broken
	// Schedule is the timer in words: "every 10 minutes".
	Schedule     string `json:"schedule"`
	TickSeconds  int    `json:"tick_seconds"`
	LastTickedAt int64  `json:"last_ticked_at,omitempty"`
	NextTickAt   int64  `json:"next_tick_at,omitempty"`
	LastRunID    string `json:"last_run_id,omitempty"`
	// Detail is whatever else matters for this worker: the queue depth, the
	// cooldown, what it will pick next.
	Detail map[string]any `json:"detail,omitempty"`
	Note   string         `json:"note,omitempty"`
}

// handleDebugOverview is the whole screen in one call: services, timers,
// dependencies and counts.
// GET /api/v2/debug/overview
func (s *Server) handleDebugOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	jsonOK(w, map[string]any{
		"now":          time.Now().Unix(),
		"services":     s.debugServices(),
		"dependencies": s.debugDependencies(),
		"engine":       s.debugEngine(),
		"llm_log":      s.store.LLMLogUsage(),
		"process":      debugProcess(),
		"database":     s.debugDatabase(),
		"active_runs":  s.runs.Active(),
	})
}

// debugServices reports every background worker on the same shape, so the
// screen can render one card per service without special cases.
func (s *Server) debugServices() []serviceInfo {
	out := []serviceInfo{}

	// The task sweep.
	if a := s.autoExtract; a != nil {
		a.mu.Lock()
		enabled, running := a.enabled(), a.running
		lastTick, lastRun := a.lastTickedAt, a.lastRunID
		hours := a.intervalHours()
		a.mu.Unlock()

		info := serviceInfo{
			Name:         "Task extraction",
			Does:         "Reads new messages in one circle at a time and proposes tasks.",
			Enabled:      enabled,
			Running:      running,
			Schedule:     "checks every 10 minutes, one circle per " + plural(hours, "hour"),
			TickSeconds:  600,
			LastTickedAt: lastTick,
			LastRunID:    lastRun,
			Detail: map[string]any{
				"interval_hours": hours,
				"due_circles":    s.dueCircleCount(),
			},
		}
		if lastTick > 0 {
			info.NextTickAt = lastTick + 600
		}
		info.Health = health(enabled, running, lastTick, 900)
		if !enabled {
			info.Note = "Switched off. Nothing is extracted automatically."
		}
		out = append(out, info)
	}

	// The meetings scanner.
	if m := s.meetingScan; m != nil {
		st := m.status()
		info := serviceInfo{
			Name: "Meeting scanner",
			Does: "Watches new messages for meeting talk, then reads the flagged chats " +
				"and proposes meetings.",
			Enabled:      st.Enabled,
			Running:      st.Running,
			Schedule:     "checks every 10 minutes, one chat per " + plural(st.CooldownHours, "hour"),
			TickSeconds:  st.TickSeconds,
			LastTickedAt: st.LastTickedAt,
			NextTickAt:   st.NextTickAt,
			LastRunID:    st.LastRunID,
			Detail: map[string]any{
				"cooldown_hours":   st.CooldownHours,
				"waiting_chats":    st.Waiting,
				"last_scanned":     st.LastScanned,
				"last_flagged":     st.LastFlagged,
				"keyword_pass":     "runs every tick, no model",
				"meetings_pending": s.pendingMeetingCount(),
			},
		}
		info.Health = health(st.Enabled, st.Running, st.LastTickedAt, 900)
		if !st.Enabled {
			info.Note = "Switched off. New meetings will not be found."
		}
		out = append(out, info)
	}

	// Media understanding.
	if mu := s.mediaUnderstanding; mu != nil {
		pending, done, failed := s.mediaCounts()
		imagesOn := mu.enabledFor("image")
		audioOn := mu.enabledFor("audio")
		info := serviceInfo{
			Name:        "Media understanding",
			Does:        "Describes images and turns voice notes into text.",
			Enabled:     imagesOn || audioOn,
			Running:     pending > 0,
			Schedule:    "drains its queue every 30 seconds",
			TickSeconds: 30,
			Detail: map[string]any{
				"images_enabled": imagesOn,
				"audio_enabled":  audioOn,
				"audio_ready":    mu.audioAvailable(),
				"codex_ready":    codexAvailable(),
				"pending":        pending,
				"done":           done,
				"failed":         failed,
			},
		}
		switch {
		case !imagesOn && !audioOn:
			info.Health = "off"
		case pending > 0:
			info.Health = "ok"
		default:
			info.Health = "idle"
		}
		if audioOn && !mu.audioAvailable() {
			info.Health = "broken"
			info.Note = "Voice notes are on, but whisper-cli was not found."
		}
		out = append(out, info)
	}

	// Profiles.
	if p := s.profiles; p != nil {
		st, _ := s.store.CountProfilesByStatus()
		out = append(out, serviceInfo{
			Name:     "Entity profiles",
			Does:     "Writes a short description of each person and group.",
			Enabled:  true,
			Running:  st.QueueSize > 0,
			Health:   healthFromQueue(st.Pending, st.QueueSize),
			Schedule: "daily refresh, plus whatever is queued",
			Detail: map[string]any{
				"total":      st.Total,
				"written":    st.OK,
				"empty":      st.Empty,
				"failed":     st.Error,
				"pending":    st.Pending,
				"stale":      st.Stale,
				"queue_size": st.QueueSize,
			},
		})
	}
	return out
}

// debugDependencies checks the outside things the bridge shells out to. Half
// of all "the AI stopped working" turns out to be one of these missing.
func (s *Server) debugDependencies() []map[string]any {
	out := []map[string]any{}

	models, err := adapters.OllamaModels(envOr("OLLAMA_URL", ""))
	ollama := map[string]any{
		"name": "Ollama",
		"what": "runs the local model that finds tasks and meetings",
		"url":  envOr("OLLAMA_URL", "http://127.0.0.1:11434"),
		"ok":   err == nil,
	}
	if err != nil {
		ollama["error"] = err.Error()
	} else {
		ollama["models"] = len(models)
	}
	out = append(out, ollama)

	out = append(out, map[string]any{
		"name": "Codex CLI",
		"what": "describes images and tidies voice-note transcripts",
		"ok":   codexAvailable(),
		"path": envOr("CODEX_BIN", "codex"),
	})

	nodePath, nodeErr := exec.LookPath("node")
	out = append(out, map[string]any{
		"name": "Node",
		"what": "runs the Claude sidecars in agent/",
		"ok":   nodeErr == nil,
		"path": nodePath,
	})
	return out
}

// debugEngine says which model is doing the work right now.
func (s *Server) debugEngine() map[string]any {
	engine, _, _ := s.store.GetSyncState(settingEngine)
	if engine == "" {
		engine = defaultEngine
	}
	model, _, _ := s.store.GetSyncState(settingModel)
	if model == "" {
		model = defaultModel
	}
	out := map[string]any{"engine": engine, "model": model}

	// Does the current engine know how to find meetings? If not, the scanner
	// will fail on every run, and it is better to say so here than to leave a
	// column of red runs for somebody to work out.
	if _, err := s.meetingFinder(); err != nil {
		out["meetings_supported"] = false
		out["meetings_error"] = err.Error()
	} else {
		out["meetings_supported"] = true
	}
	return out
}

func debugProcess() map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	return map[string]any{
		"pid":        os.Getpid(),
		"host":       host,
		"go":         runtime.Version(),
		"goroutines": runtime.NumGoroutine(),
		"heap_mb":    mem.HeapAlloc / (1024 * 1024),
		"started_at": processStart.Unix(),
		"uptime_s":   int64(time.Since(processStart).Seconds()),
		"workdir":    wd,
	}
}

var processStart = time.Now()

// debugDatabase reports the size of the things that grow.
func (s *Server) debugDatabase() map[string]any {
	out := map[string]any{}
	counts := map[string]string{
		"messages":         "messages",
		"chats":            "chats",
		"tasks":            "tasks",
		"meetings":         "meetings",
		"llm_calls":        "llm_calls",
		"service_runs":     "service_runs",
		"extraction_calls": "extraction_calls",
	}
	for label, table := range counts {
		var n int64
		s.store.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		out[label] = n
	}
	if path := envOr("WA_DB_PATH", "store/messages.db"); path != "" {
		out["path"] = path
		if fi, err := os.Stat(path); err == nil {
			out["size_mb"] = fi.Size() / (1024 * 1024)
		}
		// The write-ahead log can be bigger than the database itself when a
		// checkpoint has not run, and that surprises people.
		if fi, err := os.Stat(path + "-wal"); err == nil {
			out["wal_mb"] = fi.Size() / (1024 * 1024)
		}
	}
	return out
}

func (s *Server) dueCircleCount() int {
	if s.autoExtract == nil {
		return 0
	}
	if _, _, ok := s.autoExtract.pickDueCircle(); ok {
		return 1
	}
	return 0
}

func (s *Server) pendingMeetingCount() int {
	var n int
	s.store.DB.QueryRow(`SELECT COUNT(*) FROM meetings WHERE review_status = ?`,
		db.ReviewPending).Scan(&n)
	return n
}

func (s *Server) mediaCounts() (pending, done, failed int) {
	s.store.DB.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status IN ('pending','running') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0)
		FROM media_understanding`).Scan(&pending, &done, &failed)
	return
}

// health turns "enabled, running, last ticked" into one word the screen can
// colour. A timer that has not fired in well over its period is broken, not
// idle — that is the state worth spotting.
func health(enabled, running bool, lastTick int64, expectEverySec int64) string {
	if !enabled {
		return "off"
	}
	if running {
		return "ok"
	}
	if lastTick == 0 {
		return "idle" // has not had its first tick yet
	}
	if time.Now().Unix()-lastTick > expectEverySec*2 {
		return "broken"
	}
	return "idle"
}

func healthFromQueue(pending, running int) string {
	switch {
	case running > 0:
		return "ok"
	case pending > 0:
		return "idle"
	default:
		return "idle"
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// handleDebugRuns lists run history from the database, so it survives a
// restart — unlike the in-memory list the progress bar uses.
// GET /api/v2/debug/runs?service=&status=&limit=
func (s *Server) handleDebugRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListServiceRuns(
		r.URL.Query().Get("service"), r.URL.Query().Get("status"), limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"runs": runs})
}

// handleDebugRun returns one run with its events and the model calls it made.
// GET /api/v2/debug/runs/{id}
func (s *Server) handleDebugRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/debug/runs/"), "/")
	if id == "" {
		jsonError(w, 400, "run id required")
		return
	}
	run, err := s.store.GetServiceRun(id)
	if err != nil {
		jsonError(w, 404, "run not found")
		return
	}
	calls, _ := s.store.ListLLMCalls(db.LLMCallFilter{RunID: id, Limit: 200})
	jsonOK(w, map[string]any{"run": run, "calls": calls})
}

// handleDebugLLM lists recent model calls without their text.
// GET /api/v2/debug/llm?service=&engine=&chat=&failed=1&limit=
func (s *Server) handleDebugLLM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	calls, err := s.store.ListLLMCalls(db.LLMCallFilter{
		RunID:      q.Get("run_id"),
		Service:    q.Get("service"),
		Engine:     q.Get("engine"),
		ChatJID:    q.Get("chat"),
		OnlyFailed: q.Get("failed") == "1",
		Limit:      limit,
	})
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"calls": calls, "usage": s.store.LLMLogUsage()})
}

// handleDebugLLMCall returns one call with the full prompt and answer.
// GET /api/v2/debug/llm/{id}
func (s *Server) handleDebugLLMCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/debug/llm/"), "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		jsonError(w, 400, "call id required")
		return
	}
	call, err := s.store.GetLLMCall(id)
	if err != nil {
		jsonError(w, 404, "call not found")
		return
	}
	jsonOK(w, call)
}

// debugLogLines is how much of the bridge's own log the screen can pull.
const debugLogLines = 400

// handleDebugLogs returns the tail of the bridge's log file.
//
// The bridge runs as a LaunchAgent, so its stdout goes to a file nobody has
// open. Being able to read the last few hundred lines from the same screen as
// everything else saves a trip to the terminal.
// GET /api/v2/debug/logs?lines=200
func (s *Server) handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > debugLogLines {
		n = 200
	}

	path := bridgeLogPath()
	if path == "" {
		jsonOK(w, map[string]any{"path": "", "lines": []string{},
			"note": "No log file found. The bridge is probably running in a terminal."})
		return
	}
	lines, err := tailFile(path, n)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"path": path, "lines": lines})
}

// bridgeLogPath finds the LaunchAgent's log, or "" when there is none.
func bridgeLogPath() string {
	if p := envOr("WA_LOG_PATH", ""); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, "Library", "Logs", "whatsapp-bridge.log")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// tailFile reads the last n lines without loading a large file into memory.
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Read at most the last megabyte. A log that has grown for months is not
	// worth paging through to find its end.
	const window = 1 << 20
	start := int64(0)
	size := fi.Size()
	if size > window {
		start = size - window
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) == 0 {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:] // the first line is probably cut in half
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// startDebugHousekeeping trims the debug tables once a day.
//
// Without this the LLM log grows for ever. Fourteen days of prompts is a
// debugging window; a year of them is a second copy of every conversation.
func (s *Server) startDebugHousekeeping() {
	go func() {
		clean := func() {
			calls := s.store.PruneLLMCalls()
			runs := s.store.PruneServiceRuns()
			if calls > 0 || runs > 0 {
				fmt.Printf("Debug log cleaned: %d model call(s), %d run(s) removed\n", calls, runs)
			}
		}
		time.Sleep(5 * time.Minute)
		clean()
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for range t.C {
			clean()
		}
	}()
}

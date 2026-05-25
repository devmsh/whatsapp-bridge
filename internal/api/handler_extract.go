package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// runAgent spawns the Node sidecar (script in agent/) and returns its stdout.
// It strips ANTHROPIC_API_KEY so the sidecar uses the Claude subscription, and
// passes the bridge's DB/API/MCP locations. No live progress tracking.
func (s *Server) runAgent(timeout time.Duration, script string, args ...string) (string, error) {
	return s.runAgentTracked(context.Background(), timeout, "", nil, script, args...)
}

// runAgentInput is runAgent with stdin (used by the profile sidecar). No tracking.
func (s *Server) runAgentInput(timeout time.Duration, stdin, script string, args ...string) (string, error) {
	return s.runAgentTracked(context.Background(), timeout, stdin, nil, script, args...)
}

// runAgentTracked is the full spawner: takes an external context (so callers
// can cancel via Run.Cancel), an optional Run (whose stderr we'll consume line
// by line into RunEvents), and the usual stdin/script/args. Stderr is also
// mirrored to os.Stderr so the bridge log keeps its existing format.
func (s *Server) runAgentTracked(parentCtx context.Context, timeout time.Duration, stdin string, run *Run, script string, args ...string) (string, error) {
	cwd, _ := os.Getwd()
	nodeBin := envOr("AGENT_NODE", "node")
	scriptPath := filepath.Join(envOr("AGENT_DIR", filepath.Join(cwd, "agent")), script)
	mcpBin := envOr("WA_MCP_BIN", filepath.Join(cwd, "whatsapp-mcp"))
	dbAbs, _ := filepath.Abs(s.cfg.DBPath)
	apiURL := fmt.Sprintf("http://127.0.0.1:%d/api/v2", s.port)

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, nodeBin, append([]string{scriptPath}, args...)...)
	cmd.Dir = cwd

	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "WA_MCP_BIN="+mcpBin, "WA_DB_PATH="+dbAbs, "WA_API_URL="+apiURL)
	cmd.Env = env

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if run == nil {
		cmd.Stderr = os.Stderr
	} else {
		// Pipe stderr through a scanner that produces Run events line by line.
		pr, pw := io.Pipe()
		cmd.Stderr = pw
		done := make(chan struct{})
		go func() {
			defer close(done)
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				line := sc.Text()
				os.Stderr.WriteString(line + "\n") // keep the bridge log readable
				if e := parseProgressLine(line); e.Kind != "" {
					run.AddEvent(e)
				}
			}
		}()
		err := cmd.Run()
		pw.Close()
		<-done
		return stdout.String(), err
	}
	err := cmd.Run()
	return stdout.String(), err
}

// lastJSONLine returns the last non-empty line of out (the sidecar prints its
// JSON result on the final line; earlier lines may be logs).
func lastJSONLine(out string) []byte {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return []byte(t)
		}
	}
	return nil
}

// handleTaskExtract starts the chat-extraction agent on one chat/group.
// Returns immediately with a run_id; the work continues in the background
// and progress is available via the run's SSE stream.
// POST /api/v2/tasks/extract  {"chat_jid","group_name"} -> {"run_id"}
func (s *Server) handleTaskExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		ChatJID   string `json:"chat_jid"`
		GroupName string `json:"group_name"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ChatJID) == "" {
		jsonError(w, 400, "chat_jid required")
		return
	}
	// AI never processes hidden chats — extraction refused even when unlocked.
	if s.store.IsChatHidden(req.ChatJID) {
		jsonError(w, 403, "AI features are disabled for hidden chats")
		return
	}

	label := req.GroupName
	if label == "" {
		label = req.ChatJID
	}
	run, ctx := s.runs.Start("chat", req.ChatJID, label)
	go s.executeExtraction(ctx, run, 15*time.Minute, "extract.mjs", req.ChatJID, req.GroupName)

	fmt.Printf("Task extraction starting for %s (run=%s)\n", req.ChatJID, run.ID)
	jsonOK(w, map[string]any{"run_id": run.ID})
}

// executeExtraction is the goroutine body for any extraction sidecar call.
// It spawns the sidecar with progress tracking, parses the final JSON, and
// resolves the Run to its terminal state (Done | Failed | Cancelled).
func (s *Server) executeExtraction(ctx context.Context, run *Run, timeout time.Duration, script string, args ...string) {
	run.SetRunning()
	out, runErr := s.runAgentTracked(ctx, timeout, "", run, script, args...)

	var result struct {
		OK        bool   `json:"ok"`
		Created   int    `json:"created"`
		Summary   string `json:"summary"`
		SessionID string `json:"session_id"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &result)
	}

	if ctx.Err() == context.Canceled {
		run.Finish(RunCancelled, result.SessionID, "Cancelled by user.", result.Created, "")
		return
	}
	if runErr != nil && !result.OK {
		msg := result.Summary
		if msg == "" {
			msg = runErr.Error()
		}
		run.Finish(RunFailed, result.SessionID, msg, result.Created, msg)
		return
	}

	// Right after a successful extraction, cluster the circle's tasks. Pulled
	// out into a goroutine so we don't block the run from being marked done.
	if run.Kind == "circle" && result.Created > 0 {
		if cid, parseErr := strconv.ParseInt(run.Subject, 10, 64); parseErr == nil {
			go func() {
				if cr, err := s.clusterCircleTasks(cid); err == nil && cr != nil {
					fmt.Printf("Clustering circle %d done: %d new parents, %d reused, %d children linked\n",
						cid, cr.NewParents, cr.ReusedParents, cr.ChildrenLinked)
				}
			}()
		}
	}

	run.Finish(RunDone, result.SessionID, result.Summary, result.Created, "")
}

// handleExtractions lists past extraction runs (read from the SDK's session
// store, no DB) for either a chat or a circle.
// GET /api/v2/extractions?chat=JID    or    ?circle=ID
func (s *Server) handleExtractions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := r.URL.Query()
	var tag, match string
	switch {
	case q.Get("chat") != "":
		match = q.Get("chat")
		if !s.guardChatAccess(w, r, match) {
			return
		}
		tag = "wa-extract:" + match
	case q.Get("circle") != "":
		id := q.Get("circle")
		tag = "wa-extract-circle:" + id
		match = "circle_id " + id
	default:
		jsonError(w, 400, "chat or circle required")
		return
	}
	out, err := s.runAgent(60*time.Second, "sessions.mjs", "list", tag, match)
	if err != nil && lastJSONLine(out) == nil {
		jsonError(w, 500, "list failed: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if line := lastJSONLine(out); line != nil {
		w.Write(line)
		return
	}
	w.Write([]byte(`{"runs":[]}`))
}

// handleCircleExtract starts the circle-level extraction agent. Returns
// immediately with a run_id; progress streams via SSE.
// POST /api/v2/circles/{id}/extract -> {"run_id"}
func (s *Server) handleCircleExtract(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	circle, err := s.store.GetCircle(id)
	if err != nil || circle == nil {
		jsonError(w, 404, "circle not found")
		return
	}

	run, ctx := s.runs.Start("circle", strconv.FormatInt(id, 10), circle.Name)
	go s.executeExtraction(ctx, run, 30*time.Minute, "extract-circle.mjs",
		strconv.FormatInt(id, 10), circle.Name)

	fmt.Printf("Circle task extraction starting for circle %d (%s, run=%s)\n", id, circle.Name, run.ID)
	jsonOK(w, map[string]any{"run_id": run.ID})
}

// handleExtractionTranscript returns a run's full transcript from the SDK.
// GET /api/v2/extractions/transcript?session=ID
func (s *Server) handleExtractionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	session := r.URL.Query().Get("session")
	if session == "" {
		jsonError(w, 400, "session required")
		return
	}
	out, err := s.runAgent(60*time.Second, "sessions.mjs", "show", session)
	if err != nil && lastJSONLine(out) == nil {
		jsonError(w, 500, "transcript failed: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if line := lastJSONLine(out); line != nil {
		w.Write(line)
		return
	}
	w.Write([]byte(`{"steps":[]}`))
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
// passes the bridge's DB/API/MCP locations.
func (s *Server) runAgent(timeout time.Duration, script string, args ...string) (string, error) {
	return s.runAgentInput(timeout, "", script, args...)
}

// runAgentInput is runAgent with optional data piped to the sidecar's stdin.
func (s *Server) runAgentInput(timeout time.Duration, stdin, script string, args ...string) (string, error) {
	cwd, _ := os.Getwd()
	nodeBin := envOr("AGENT_NODE", "node")
	scriptPath := filepath.Join(envOr("AGENT_DIR", filepath.Join(cwd, "agent")), script)
	mcpBin := envOr("WA_MCP_BIN", filepath.Join(cwd, "whatsapp-mcp"))
	dbAbs, _ := filepath.Abs(s.cfg.DBPath)
	apiURL := fmt.Sprintf("http://127.0.0.1:%d/api/v2", s.port)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	cmd.Stderr = os.Stderr
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

// handleTaskExtract runs the extraction agent on one chat/group (Max-plan auth).
// POST /api/v2/tasks/extract  {"chat_jid","group_name"}
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

	fmt.Printf("Task extraction starting for %s\n", req.ChatJID)
	out, runErr := s.runAgent(15*time.Minute, "extract.mjs", req.ChatJID, req.GroupName)

	var result struct {
		OK        bool   `json:"ok"`
		Created   int    `json:"created"`
		Summary   string `json:"summary"`
		SessionID string `json:"session_id"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &result)
	}
	if runErr != nil && !result.OK {
		msg := result.Summary
		if msg == "" {
			msg = runErr.Error()
		}
		jsonError(w, 500, "extraction failed: "+msg)
		return
	}
	jsonOK(w, map[string]interface{}{
		"ok":         result.OK,
		"created":    result.Created,
		"summary":    result.Summary,
		"session_id": result.SessionID,
	})
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

// handleCircleExtract runs the circle-level extraction agent (Max-plan auth).
// POST /api/v2/circles/{id}/extract
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

	fmt.Printf("Circle task extraction starting for circle %d (%s)\n", id, circle.Name)
	out, runErr := s.runAgent(30*time.Minute, "extract-circle.mjs", strconv.FormatInt(id, 10), circle.Name)

	var result struct {
		OK        bool   `json:"ok"`
		Created   int    `json:"created"`
		Summary   string `json:"summary"`
		SessionID string `json:"session_id"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &result)
	}
	if runErr != nil && !result.OK {
		msg := result.Summary
		if msg == "" {
			msg = runErr.Error()
		}
		jsonError(w, 500, "extraction failed: "+msg)
		return
	}
	jsonOK(w, map[string]interface{}{
		"ok":         result.OK,
		"created":    result.Created,
		"summary":    result.Summary,
		"session_id": result.SessionID,
	})
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

package api

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Codex is the local OpenAI Codex CLI, authenticated via the ChatGPT
// subscription in ~/.codex (NOT the OpenAI API — so these calls don't bill an
// API key, mirroring how the rest of the AI uses the Claude subscription).
//
// We use it for media understanding (image description + voice-note transcript
// refinement) instead of the Claude Agent SDK. Each call is a one-shot
// `codex exec`: prompt on stdin, optional images via -i, and the model's final
// message captured with --output-last-message. It runs read-only in a temp dir
// so a prompt-injected image/transcript can never touch the repo.

const codexDefaultMaxConcurrent = 2

var (
	codexSemOnce sync.Once
	codexSem     chan struct{}
	codexFenceRe = regexp.MustCompile("(?s)^```(?:\\w+)?\\s*(.+?)\\s*```$")
)

// acquireCodex bounds how many `codex exec` processes run at once across all
// media workers, so backfilling the history doesn't flood the ChatGPT/Codex
// quota. Capacity comes from CODEX_MAX_CONCURRENT (default 2). The returned
// func releases the slot.
func acquireCodex() func() {
	codexSemOnce.Do(func() {
		n := codexDefaultMaxConcurrent
		if v := envOr("CODEX_MAX_CONCURRENT", ""); v != "" {
			if k, err := strconv.Atoi(v); err == nil && k > 0 {
				n = k
			}
		}
		codexSem = make(chan struct{}, n)
	})
	codexSem <- struct{}{}
	return func() { <-codexSem }
}

// codexAvailable reports whether the Codex CLI is on PATH (or CODEX_BIN).
func codexAvailable() bool {
	_, err := exec.LookPath(envOr("CODEX_BIN", "codex"))
	return err == nil
}

// codexExec runs a single-shot Codex call. The prompt is piped on stdin;
// optional images are attached with -i. The model's final message is captured
// via --output-last-message and returned (code fences stripped). The timeout
// covers only the exec itself — time spent waiting for a concurrency slot does
// not count against it.
func codexExec(timeout time.Duration, prompt string, imagePaths ...string) (string, error) {
	bin := envOr("CODEX_BIN", "codex")
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("codex CLI not found (install it or set CODEX_BIN)")
	}

	release := acquireCodex()
	defer release()

	outF, err := os.CreateTemp("", "codex-out-*.txt")
	if err != nil {
		return "", fmt.Errorf("codex temp file: %w", err)
	}
	outPath := outF.Name()
	outF.Close()
	defer os.Remove(outPath)

	workDir, err := os.MkdirTemp("", "codex-cwd-*")
	if err != nil {
		return "", fmt.Errorf("codex temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	args := []string{
		"exec",
		"--ephemeral",            // don't persist a session to disk
		"--skip-git-repo-check",  // we run in a throwaway temp dir
		"--sandbox", "read-only", // never let it write or run commands
		"--color", "never",
		"-o", outPath,
	}
	if model := envOr("CODEX_MODEL", ""); model != "" {
		args = append(args, "-m", model)
	}
	for _, p := range imagePaths {
		if p != "" {
			args = append(args, "-i", p)
		}
	}

	ctx, cancel := newTimeoutCtx(timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workDir // isolate from the repo (no AGENTS.md / project context)
	cmd.Stdin = strings.NewReader(prompt)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	// stdout is the event stream we don't need; leaving it nil discards it.
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex exec: %v: %s", err, snippet(errBuf.String(), 240))
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		return "", fmt.Errorf("read codex output: %w", err)
	}
	out := strings.TrimSpace(string(b))
	if m := codexFenceRe.FindStringSubmatch(out); m != nil {
		out = strings.TrimSpace(m[1])
	}
	return out, nil
}

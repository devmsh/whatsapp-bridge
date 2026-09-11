package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runSidecar starts the Node script that asks Claude one question.
//
// This is the same spawn the bridge does, minus the progress reporting the
// bridge needs for its UI. ANTHROPIC_API_KEY is removed on purpose: the
// sidecar must use the Max subscription, and a key in the environment would
// quietly start billing instead.
func runSidecar(parent context.Context, stdin, script string, args ...string) (string, error) {
	cwd, _ := os.Getwd()
	node := envOr("AGENT_NODE", "node")
	path := filepath.Join(envOr("AGENT_DIR", filepath.Join(cwd, "agent")), script)

	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, node, append([]string{path}, args...)...)
	cmd.Dir = cwd

	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("%s: %w", script, err)
	}
	return out.String(), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

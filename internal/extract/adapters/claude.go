package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"whatsapp-bridge-v2/internal/extract"
)

// Claude, doing exactly the same single job as the local model.
//
// Note what this is NOT: the old extractor was an agent with tools and 120
// turns, and it is being replaced precisely because that shape cannot be
// checked or ported. This adapter asks one question and reads one answer, so
// the two engines are comparable — switching between them measures the model,
// not two different architectures.
//
// It stays available for the cases where the local model is unsure, and as the
// yardstick the evaluation command measures against.

// AgentRunner spawns the Node sidecar. The API layer owns the spawning (it
// knows where the scripts and the binaries live), so the adapter takes it as a
// function rather than reaching for it.
type AgentRunner func(ctx context.Context, stdin, script string, args ...string) (string, error)

type Claude struct {
	run   AgentRunner
	model string
}

func NewClaude(run AgentRunner, model string) *Claude {
	return &Claude{run: run, model: model}
}

func (c *Claude) Name() string {
	if c.model == "" {
		return "claude:chunk"
	}
	return "claude:" + c.model
}

func (c *Claude) Extract(ctx context.Context, in extract.ExtractInput) (extract.ExtractOutput, error) {
	var out extract.ExtractOutput
	err := c.ask(ctx,
		fmt.Sprintf(extractSystem, rosterBlock(in)),
		"Conversation slice:\n\n"+in.Chunk.Rendered,
		extractSchema, &out)
	return out, err
}

func (c *Claude) CheckCompletion(ctx context.Context, in extract.CompletionInput) (extract.CompletionOutput, error) {
	if len(in.Open) == 0 {
		return extract.CompletionOutput{}, nil
	}
	var b strings.Builder
	b.WriteString("Open tasks:\n")
	for _, t := range in.Open {
		b.WriteString("- id " + strconv.FormatInt(t.ID, 10) + ": " + t.Title)
		if t.OwnerName != "" {
			b.WriteString(" (owner: " + t.OwnerName + ")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nConversation slice:\n\n")
	b.WriteString(in.Chunk.Rendered)

	var out extract.CompletionOutput
	err := c.ask(ctx, completionSystem, b.String(), completionSchema, &out)
	return out, err
}

func (c *Claude) ask(ctx context.Context, system, user string, schema any, dst any) error {
	if c.run == nil {
		return fmt.Errorf("no agent runner configured")
	}
	payload, err := json.Marshal(map[string]any{
		"system": system,
		"user":   user,
		"schema": schema,
		"model":  c.model,
	})
	if err != nil {
		return err
	}

	out, err := c.run(ctx, string(payload), "extract-chunk.mjs")
	if err != nil && strings.TrimSpace(out) == "" {
		return fmt.Errorf("claude sidecar: %w", err)
	}

	// The sidecar prints progress on stderr and one JSON object on stdout, so
	// the last non-empty line is the answer.
	line := lastLine(out)
	if line == "" {
		return fmt.Errorf("claude sidecar returned nothing")
	}
	if err := json.Unmarshal([]byte(line), dst); err != nil {
		return fmt.Errorf("claude sidecar returned invalid JSON: %w", err)
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/extract"
)

// Ollama runs the detection step on a model on this machine.
//
// It is the default engine. The work it does — read a slice of chat, say what
// work is in it — is the one part of extraction that needs judgement, and a
// 14B model on an M-series Mac does it in tens of seconds with no quota and no
// network. Everything it returns is checked afterwards, which is what makes a
// local model good enough.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllama(baseURL, model string) *Ollama {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	if model == "" {
		model = "qwen2.5:14b"
	}
	return &Ollama{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		// Generous: a long chunk on a big model can take minutes, and a
		// timeout here throws away work the pipeline cannot redo cheaply.
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (o *Ollama) Name() string { return "ollama:" + o.model }

func (o *Ollama) Extract(ctx context.Context, in extract.ExtractInput) (extract.ExtractOutput, error) {
	system := fmt.Sprintf(extractSystem, rosterBlock(in))
	user := "Conversation slice:\n\n" + in.Chunk.Rendered

	var out extract.ExtractOutput
	if err := o.chat(ctx, system, user, extractSchema, &out); err != nil {
		return extract.ExtractOutput{}, err
	}
	return out, nil
}

func (o *Ollama) Judge(ctx context.Context, in extract.JudgeInput) (extract.JudgeOutput, error) {
	if len(in.Items) == 0 {
		return extract.JudgeOutput{}, nil
	}
	var out extract.JudgeOutput
	if err := o.chat(ctx, judgeSystem, judgeUser(in), judgeSchema, &out); err != nil {
		return extract.JudgeOutput{}, err
	}
	return out, nil
}

func (o *Ollama) CheckCompletion(ctx context.Context, in extract.CompletionInput) (extract.CompletionOutput, error) {
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
	if err := o.chat(ctx, completionSystem, b.String(), completionSchema, &out); err != nil {
		return extract.CompletionOutput{}, err
	}
	return out, nil
}

// rosterBlock is the "who is here" preamble. Empty is fine — a DM with an
// unknown number still has messages worth reading.
func rosterBlock(in extract.ExtractInput) string {
	block := extract.RenderRoster(in.Roster, in.OwnName)
	if in.ChatName != "" {
		kind := "direct chat"
		if in.IsGroup {
			kind = "group"
		}
		block = "This is the " + kind + ` "` + in.ChatName + `".` + "\n" + block
	}
	return block + "\n"
}

type ollamaRequest struct {
	Model     string          `json:"model"`
	Messages  []ollamaMessage `json:"messages"`
	Stream    bool            `json:"stream"`
	Format    any             `json:"format,omitempty"`
	Options   map[string]any  `json:"options,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
	Error   string        `json:"error"`
}

// chat calls the model once and unmarshals its JSON into dst.
//
// Retries once on unusable JSON. Structured output makes that rare, but a
// model can still return an empty string under load, and one retry is much
// cheaper than losing the chunk.
func (o *Ollama) chat(ctx context.Context, system, user string, schema any, dst any) error {
	body := ollamaRequest{
		Model: o.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
		Format: schema,
		Options: map[string]any{
			// Extraction is not a creative task. The same chat should give the
			// same answer twice, or the eval numbers mean nothing.
			"temperature": 0,
			"num_ctx":     16384,
		},
		// Keep the weights resident between chunks; reloading an 9GB model per
		// call costs more than the call.
		KeepAlive: "30m",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			o.baseURL+"/api/chat", bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ollama unreachable at %s: %w", o.baseURL, err)
			continue
		}
		var parsed ollamaResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if decodeErr != nil {
			lastErr = fmt.Errorf("ollama returned unreadable response: %w", decodeErr)
			continue
		}
		if parsed.Error != "" {
			// A bad model name or an out-of-memory is not worth retrying.
			return fmt.Errorf("ollama: %s", parsed.Error)
		}
		content := strings.TrimSpace(parsed.Message.Content)
		if content == "" {
			lastErr = fmt.Errorf("ollama returned an empty answer")
			continue
		}
		if err := json.Unmarshal([]byte(content), dst); err != nil {
			lastErr = fmt.Errorf("ollama returned invalid JSON: %w", err)
			continue
		}
		return nil
	}
	return lastErr
}

// OllamaModels lists what is installed locally, so a settings screen can offer
// real choices instead of asking somebody to type a model name correctly.
func OllamaModels(baseURL string) ([]string, error) {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama is not reachable: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				Family string `json:"family"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		// Embedding models cannot answer a chat prompt; offering them would
		// only produce a confusing failure later.
		if strings.Contains(m.Name, "embed") || m.Details.Family == "bert" {
			continue
		}
		out = append(out, m.Name)
	}
	return out, nil
}

package adapters_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"whatsapp-bridge-v2/internal/extract"
	"whatsapp-bridge-v2/internal/extract/adapters"
)

func chunk() extract.Chunk {
	return extract.Chunk{
		ChatJID:  "team@g.us",
		Rendered: "[#M1] [Mon 27 Jul 11:00] Sara: جهز العقد\n",
		Lines: []extract.Line{
			{MessageID: "M1", Sender: "Sara", Text: "جهز العقد"},
		},
	}
}

// TestOllamaSendsSchemaAndRoster checks the two things the request must carry:
// a schema, so the answer is parseable at all, and the roster, so the model can
// name an owner instead of inventing one.
func TestOllamaSendsSchemaAndRoster(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"message":{"role":"assistant","content":"{\"tasks\":[]}"}}`)
	}))
	defer srv.Close()

	o := adapters.NewOllama(srv.URL, "qwen2.5:14b")
	if o.Name() != "ollama:qwen2.5:14b" {
		t.Errorf("Name() = %q, want the engine and model", o.Name())
	}
	_, err := o.Extract(context.Background(), extract.ExtractInput{
		Chunk:    chunk(),
		ChatName: "The 3 committee",
		IsGroup:  true,
		OwnName:  "Mohammed",
		Roster: []extract.RosterPerson{
			{JID: "1@s.whatsapp.net", Name: "Sara Haddad", Kunya: "أم يوسف"},
		},
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request was not JSON: %v", err)
	}
	if sent["format"] == nil {
		t.Errorf("no schema sent — the answer would be unparseable")
	}
	if sent["stream"] != false {
		t.Errorf("streaming must be off; one call, one answer")
	}
	opts, _ := sent["options"].(map[string]any)
	if opts == nil || opts["temperature"] != float64(0) {
		t.Errorf("temperature must be 0, or the same chat gives different answers")
	}

	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected a system and a user message, got %d", len(msgs))
	}
	system, _ := msgs[0].(map[string]any)["content"].(string)
	for _, want := range []string{"Sara Haddad", "أم يوسف", "The 3 committee", "Past tense"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	user, _ := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(user, "[#M1]") {
		t.Errorf("the chunk, with its message ids, must reach the model")
	}
}

// TestOllamaRetriesBadJSONOnce — structured output makes this rare, but a
// model under load can return nothing, and one retry is far cheaper than
// losing the chunk.
func TestOllamaRetriesBadJSONOnce(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			io.WriteString(w, `{"message":{"role":"assistant","content":"not json"}}`)
			return
		}
		io.WriteString(w, `{"message":{"role":"assistant","content":"{\"tasks\":[{\"title\":\"جهز العقد\",\"owner_text\":\"Sara\",\"evidence_id\":\"#M1\",\"evidence\":\"جهز العقد\",\"due_text\":\"\",\"priority_hint\":\"normal\",\"confidence\":0.9}]}"}}`)
	}))
	defer srv.Close()

	out, err := adapters.NewOllama(srv.URL, "m").
		Extract(context.Background(), extract.ExtractInput{Chunk: chunk()})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected exactly one retry, got %d calls", calls)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].EvidenceID != "#M1" {
		t.Fatalf("the retried answer should be used: %+v", out.Tasks)
	}
}

// TestOllamaDoesNotRetryModelErrors — a missing model or an out-of-memory will
// fail the same way twice. Retrying only doubles the wait.
func TestOllamaDoesNotRetryModelErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		io.WriteString(w, `{"error":"model 'nope' not found"}`)
	}))
	defer srv.Close()

	_, err := adapters.NewOllama(srv.URL, "nope").
		Extract(context.Background(), extract.ExtractInput{Chunk: chunk()})
	if err == nil {
		t.Fatalf("a model error should surface, not be swallowed")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("model errors must not be retried, got %d calls", calls)
	}
}

// TestCompletionSkipsTheCallWithNothingOpen — no open tasks means there is
// nothing a message could complete, so the model is not worth waking.
func TestCompletionSkipsTheCallWithNothingOpen(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		io.WriteString(w, `{"message":{"content":"{\"completions\":[]}"}}`)
	}))
	defer srv.Close()

	out, err := adapters.NewOllama(srv.URL, "m").CheckCompletion(
		context.Background(), extract.CompletionInput{Chunk: chunk()})
	if err != nil {
		t.Fatalf("CheckCompletion: %v", err)
	}
	if len(out.Completions) != 0 || calls != 0 {
		t.Errorf("with no open tasks there is nothing to ask about (calls=%d)", calls)
	}
}

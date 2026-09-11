// Package extract turns a WhatsApp conversation into reviewed tasks.
//
// The split it is built on: if a step has a right answer, code does it; if a
// step needs judgement, the model does it, and code checks the answer
// afterwards. So this package owns the database, the dates, the owners and the
// duplicates, and the model is asked exactly one thing — "what work does this
// chunk of chat contain, and which message says so".
//
// The model never touches the database. It receives text and returns JSON.
// Everything with a side effect happens here.
package extract

import "context"

// RosterPerson is somebody in the conversation, as the model should see them.
//
// Kunya and HowWeMet are the user's own notes. They are the difference between
// "a number" and "أبو يمان, who Abdullah introduced about the CVB file" — and
// the model has no other way to learn either.
type RosterPerson struct {
	JID      string
	Name     string
	Kunya    string
	HowWeMet string
	IsAdmin  bool
}

// Line is one message, already enriched: voice notes carry their transcript,
// images their description, mentions their resolved names.
type Line struct {
	MessageID string
	TS        int64
	Sender    string
	SenderJID string
	// Text is what the model reads — the typed text, or the transcript, or
	// the image description, merged the same way the MCP read tools do it.
	Text string
	// Mentions holds the JIDs the message @-mentioned, in phone form where a
	// mapping exists. This is how an owner gets resolved without guessing.
	Mentions  []string
	ReplyTo   string
	Forwarded bool
	// Context marks a line carried in only so a reply or a thread makes sense.
	// The model may read it; it may not draw a task from it.
	Context bool
	// MediaType is kept so verification can be lenient about transcripts,
	// which a model sometimes tidies up while quoting.
	MediaType string
}

// Chunk is what one model call sees.
type Chunk struct {
	ChatJID  string
	Index    int
	Lines    []Line
	Rendered string
}

// ProposedTask is the model's answer. Every field is a claim to be checked:
// the evidence must really appear in the named message, the owner is only a
// hint until code resolves it, and the due date is a phrase, not a timestamp.
type ProposedTask struct {
	Title        string  `json:"title"`
	OwnerText    string  `json:"owner_text"`
	EvidenceID   string  `json:"evidence_id"`
	Evidence     string  `json:"evidence"`
	DueText      string  `json:"due_text"`
	PriorityHint string  `json:"priority_hint"`
	Confidence   float64 `json:"confidence"`
}

// OpenTask is an existing task the model is asked to watch for completions of.
type OpenTask struct {
	ID        int64
	Title     string
	OwnerName string
	// OriginChatJID and OriginMessageID let code spot a done-reply without
	// asking the model at all.
	OriginChatJID   string
	OriginMessageID string
}

// ProposedCompletion is the model saying "this message reports that task done".
type ProposedCompletion struct {
	TaskID     int64   `json:"task_id"`
	EvidenceID string  `json:"evidence_id"`
	Evidence   string  `json:"evidence"`
	Confidence float64 `json:"confidence"`
}

type ExtractInput struct {
	Chunk    Chunk
	Roster   []RosterPerson
	OwnName  string
	ChatName string
	IsGroup  bool
}

type ExtractOutput struct {
	Tasks []ProposedTask `json:"tasks"`
}

type CompletionInput struct {
	Chunk Chunk
	Open  []OpenTask
}

type CompletionOutput struct {
	Completions []ProposedCompletion `json:"completions"`
}

// Extractor is the one volatile layer: the model itself.
//
// Plain data in, plain data out. An implementation may not open the database
// or call the bridge API — that is what keeps a model swap to one file, and
// what lets the pipeline be tested with a fake that returns fixed answers.
type Extractor interface {
	// Name identifies the engine and model for the record, e.g.
	// "ollama:qwen2.5:14b".
	Name() string
	Extract(ctx context.Context, in ExtractInput) (ExtractOutput, error)
	CheckCompletion(ctx context.Context, in CompletionInput) (CompletionOutput, error)
}

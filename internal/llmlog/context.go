package llmlog

import "context"

// The run a model call belongs to travels on the context.
//
// An adapter is handed a chunk of text and nothing else — by design, since it
// may not reach into the database or the API layer. But a log entry is useless
// without "which run, which chat", so the pipeline attaches that to the
// context it already passes down, and the adapter reads it back out.

type ctxKey struct{}

// Tag is what the pipeline knows and the adapter does not.
type Tag struct {
	RunID   string
	Service string // tasks | meetings | media | profiles
	ChatJID string
}

// With attaches the tag to a context. Everything below it is recorded under
// this run.
func With(ctx context.Context, t Tag) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// From reads the tag back. An untagged context gives a zero Tag, which records
// fine — just without a run to group it under.
func From(ctx context.Context) Tag {
	if ctx == nil {
		return Tag{}
	}
	t, _ := ctx.Value(ctxKey{}).(Tag)
	return t
}

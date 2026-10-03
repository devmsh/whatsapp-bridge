package extract

import "context"

// The "what changed?" question, asked separately from "find meetings" (see
// docs/superpowers/specs/2026-09-18-live-meetings-design.md, decision 1).
//
// A meeting found once goes stale unless something reads the chat again. This
// port is that second read: the model is told the meetings already open in a
// chat, and shown a later slice of the same chat, and it says which of those
// meetings this slice changes, and how. Code still checks every claim and
// still computes every date — the model only reports the words that were
// said.

// KnownMeeting is one open meeting the model is told about, so it can say
// what a later slice of chat changes about it.
type KnownMeeting struct {
	Ref     int // 1-based number inside this call; the model answers with it
	ID      int64
	Title   string
	Purpose string
	Status  string
	// When is the meeting's time in words a human reads: "2026-09-18 Friday
	// 17:00", or "no date yet". The timing words actually said are added in
	// brackets when the meeting has any, so the model sees both what code
	// resolved and what people actually typed.
	When      string
	Mode      string
	Location  string
	Link      string
	Agenda    []string
	Attendees []string
	// StartsAt is the same date as When, as a timestamp. It is never shown to
	// the model — the prompt only carries When — but VerifyUpdate needs it to
	// check that a meeting reported "held" was not still in the future.
	StartsAt int64
}

// MeetingUpdateInput is one chunk of chat, plus the meetings that were still
// open when it was sent.
type MeetingUpdateInput struct {
	Chunk    Chunk
	Known    []KnownMeeting
	Roster   []RosterPerson
	OwnName  string
	ChatName string
	IsGroup  bool
	// Today is the date the run happens, as YYYY-MM-DD, same as MeetingInput.
	Today string
}

// ProposedUpdate is the model's claim: this slice of chat changes one known
// meeting, in this way, and here is the one message that proves it.
type ProposedUpdate struct {
	Ref int `json:"ref"`
	// Status is "" when it did not change, otherwise one of proposed,
	// confirmed, cancelled, held.
	Status string `json:"status"`
	// WhenText / TimeText are new timing words, exactly as said. "" means
	// that part did not change. Never a computed date.
	WhenText string `json:"when_text"`
	TimeText string `json:"time_text"`
	// Postponed is true only when the meeting moved with NO new day named.
	Postponed  bool     `json:"postponed"`
	Mode       string   `json:"mode"`     // "" = no change
	Location   string   `json:"location"` // "" = no change
	Link       string   `json:"link"`     // "" = no change
	AgendaAdd  []string `json:"agenda_add"`
	Note       string   `json:"note"` // one short sentence, in the chat's language
	EvidenceID string   `json:"evidence_id"`
	Evidence   string   `json:"evidence"`
	Confidence float64  `json:"confidence"`
}

type MeetingUpdateOutput struct {
	Updates []ProposedUpdate `json:"updates"`
}

// MeetingUpdater is the "what changed" half of an engine.
//
// Kept apart from MeetingFinder for the same reason MeetingFinder is kept
// apart from Extractor: an engine can be good at one job and not the other,
// and each question needs its own measurement.
type MeetingUpdater interface {
	Name() string
	UpdateMeetings(ctx context.Context, in MeetingUpdateInput) (MeetingUpdateOutput, error)
}

// UpdateClaim is the second opinion's question, and it is a different
// question from the updater's — same trade as JudgeItem is to ProposedTask
// (see port.go). The updater is asked "what changed"; the judge is asked to
// check ONE claim about ONE meeting, built by code from what the updater
// already said, against a short window of the real chat. Same model,
// opposite pressure: the updater is rewarded for finding a change, the judge
// for saying a claimed change is not really there.
type UpdateClaim struct {
	Known KnownMeeting
	// Claim is one plain English sentence, built by code (see
	// claimForUpdate in meetings_update.go), never by the model — the model
	// that made the claim does not get to also phrase the question about it.
	Claim string
	// Evidence is the rendered evidence line, plus up to 3 rendered lines
	// before it and 3 after, in order — enough to see the message in its own
	// context, not the whole chunk.
	Evidence string
	Today    string
	// Settled marks a "the chat already settled it" claim. It gets its own,
	// simpler question: one prompt covering every kind of claim made a local
	// model apply the rules about time words to a claim with no time in it.
	Settled bool
}

// UpdateVerdict answers UpdateClaim: does the evidence really say what the
// claim says it does.
type UpdateVerdict struct {
	Agrees bool   `json:"agrees"`
	Reason string `json:"reason"`
}

// MeetingUpdateJudge is an OPTIONAL second half of an engine: a check on its
// own UpdateMeetings answers before a date or status change is applied. It is
// optional because it is a second model call for every timing or status
// change — worth it for a meeting, where a wrong date is a false commitment,
// not worth building for every engine that only ever runs in a test.
type MeetingUpdateJudge interface {
	JudgeUpdate(ctx context.Context, in UpdateClaim) (UpdateVerdict, error)
}

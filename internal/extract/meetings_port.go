package extract

import "context"

// Finding meetings on the same engine that finds tasks.
//
// The old meeting finder was an agent with ten tools and 120 turns, running on
// the Claude subscription. It worked, but it could not be scheduled: one busy
// group took most of an hour, so nothing ever ran it automatically and new
// meetings were simply missed. See docs/backlog.md.
//
// This is the same trade the task engine already made. The model is asked one
// question about one slice of chat — "is a meeting being arranged here, and
// which message says so" — and everything that has a right answer is done in
// code afterwards: the date, the people, the join code, the duplicates.
//
// What is deliberately given up: the old agent could search OTHER chats for
// the same join link and stitch one meeting across three conversations. Code
// does that instead, and more cheaply — the join code is a database lookup
// (FindMeetingByCode), not a model call. What code cannot do is find the same
// meeting across chats when there is NO link, only words. That case now waits
// for review rather than being merged automatically, which is the safer error.

// ProposedMeeting is the model's answer about one meeting. Like a proposed
// task, every field is a claim: the quote must really be in the message named,
// the time is words rather than a timestamp, and the attendees are names to be
// matched against the roster, never JIDs.
type ProposedMeeting struct {
	Title   string `json:"title"`
	Purpose string `json:"purpose"`
	// EvidenceID and Evidence anchor the meeting to one real message, exactly
	// as they do for a task. Without them nothing can be checked.
	EvidenceID string `json:"evidence_id"`
	Evidence   string `json:"evidence"`
	// WhenText is the day as it was said: "الخميس", "بكرة", "next week", or ""
	// when nobody named one. TimeText is the clock time as said: "٥ العصر",
	// "2pm", "" when none. Never a computed date — that is code's job.
	WhenText string `json:"when_text"`
	TimeText string `json:"time_text"`
	// Status is "proposed" while a time is still being argued over — or when
	// no time has been raised at all — and "confirmed" once one is agreed.
	Status string `json:"status"`
	// Mode is "online", "in_person" or "" when the chat does not say.
	Mode     string `json:"mode"`
	Location string `json:"location"`
	Link     string `json:"link"`
	// Attendees are names as the chat writes them. Code resolves them against
	// the roster; an unmatched name is dropped, never invented into a JID.
	Attendees  []string `json:"attendees"`
	Agenda     []string `json:"agenda"`
	Confidence float64  `json:"confidence"`
}

// MeetingInput is one chunk of chat, with who is in it.
type MeetingInput struct {
	Chunk    Chunk
	Roster   []RosterPerson
	OwnName  string
	ChatName string
	IsGroup  bool
	// Today is the date the run happens, as YYYY-MM-DD. The model is told it
	// so "الخميس" reads as a day ahead rather than an abstraction; the actual
	// arithmetic still happens in code.
	Today string
}

type MeetingOutput struct {
	Meetings []ProposedMeeting `json:"meetings"`
}

// MeetingFinder is the meetings half of an engine.
//
// Kept separate from Extractor rather than bolted onto it: an engine can be
// good at one job and not the other, and the evaluation command needs to
// measure them apart. An engine that does not implement this simply cannot
// find meetings, and the caller says so instead of failing.
type MeetingFinder interface {
	Name() string
	FindMeetings(ctx context.Context, in MeetingInput) (MeetingOutput, error)
}

package adapters

import (
	"context"
	"fmt"
	"strings"

	"whatsapp-bridge-v2/internal/extract"
)

// Both engines answer the meetings question the same way they answer the tasks
// one: one call, one chunk, JSON back. Sharing the prompt is what makes
// switching engines a measurement of the model rather than of two designs.

func (o *Ollama) FindMeetings(ctx context.Context, in extract.MeetingInput) (extract.MeetingOutput, error) {
	var out extract.MeetingOutput
	if err := o.chat(ctx, "meetings",
		fmt.Sprintf(meetingSystem, meetingRosterBlock(in), in.Today),
		"Conversation slice:\n\n"+in.Chunk.Rendered,
		meetingSchema, &out); err != nil {
		return extract.MeetingOutput{}, err
	}
	return out, nil
}

func (c *Claude) FindMeetings(ctx context.Context, in extract.MeetingInput) (extract.MeetingOutput, error) {
	var out extract.MeetingOutput
	err := c.ask(ctx, "meetings",
		fmt.Sprintf(meetingSystem, meetingRosterBlock(in), in.Today),
		"Conversation slice:\n\n"+in.Chunk.Rendered,
		meetingSchema, &out)
	return out, err
}

// meetingRosterBlock is rosterBlock again, for the meetings input shape. The
// two inputs carry the same four fields but are separate types, because a
// meetings engine and a tasks engine should be swappable one at a time.
func meetingRosterBlock(in extract.MeetingInput) string {
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

// The "what changed?" question — same engines, same call shape, a different
// prompt (prompts_meetings_update.go).

func (o *Ollama) UpdateMeetings(ctx context.Context, in extract.MeetingUpdateInput) (extract.MeetingUpdateOutput, error) {
	var out extract.MeetingUpdateOutput
	if err := o.chat(ctx, "meeting-updates",
		fmt.Sprintf(meetingUpdateSystem, meetingUpdateRosterBlock(in), in.Today),
		meetingUpdateUser(in), meetingUpdateSchema, &out); err != nil {
		return extract.MeetingUpdateOutput{}, err
	}
	return out, nil
}

func (c *Claude) UpdateMeetings(ctx context.Context, in extract.MeetingUpdateInput) (extract.MeetingUpdateOutput, error) {
	var out extract.MeetingUpdateOutput
	err := c.ask(ctx, "meeting-updates",
		fmt.Sprintf(meetingUpdateSystem, meetingUpdateRosterBlock(in), in.Today),
		meetingUpdateUser(in), meetingUpdateSchema, &out)
	return out, err
}

// meetingUpdateRosterBlock is meetingRosterBlock again, for the update
// input's own type.
func meetingUpdateRosterBlock(in extract.MeetingUpdateInput) string {
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

// meetingUpdateUser renders the user message: the known meetings the model
// must check against, then the chat slice itself.
func meetingUpdateUser(in extract.MeetingUpdateInput) string {
	var b strings.Builder
	b.WriteString("KNOWN MEETINGS:\n")
	if len(in.Known) == 0 {
		b.WriteString("(none)\n")
	}
	for _, k := range in.Known {
		fmt.Fprintf(&b, "%d. %s — %s\n", k.Ref, k.Title, k.When)
		if k.Purpose != "" {
			b.WriteString("   purpose: " + k.Purpose + "\n")
		}
		b.WriteString("   status: " + k.Status + "\n")
		if k.Mode != "" {
			b.WriteString("   mode: " + k.Mode + "\n")
		}
		if k.Location != "" {
			b.WriteString("   location: " + k.Location + "\n")
		}
		if k.Link != "" {
			b.WriteString("   link: " + k.Link + "\n")
		}
		if len(k.Agenda) > 0 {
			b.WriteString("   agenda: " + strings.Join(k.Agenda, "; ") + "\n")
		}
		if len(k.Attendees) > 0 {
			b.WriteString("   attendees: " + strings.Join(k.Attendees, ", ") + "\n")
		}
	}
	b.WriteString("\nConversation slice:\n\n")
	b.WriteString(in.Chunk.Rendered)
	return b.String()
}

// The second opinion (round 3): a different, narrow question about one
// update the updater already proposed, before it is applied. Same call
// shape as UpdateMeetings, a different prompt (meetingUpdateJudgeSystem).

func (o *Ollama) JudgeUpdate(ctx context.Context, in extract.UpdateClaim) (extract.UpdateVerdict, error) {
	var out extract.UpdateVerdict
	if err := o.chat(ctx, "meeting-update-judge",
		fmt.Sprintf(judgeSystemFor(in), in.Today),
		meetingUpdateJudgeUser(in), meetingUpdateJudgeSchema, &out); err != nil {
		return extract.UpdateVerdict{}, err
	}
	return out, nil
}

func (c *Claude) JudgeUpdate(ctx context.Context, in extract.UpdateClaim) (extract.UpdateVerdict, error) {
	var out extract.UpdateVerdict
	err := c.ask(ctx, "meeting-update-judge",
		fmt.Sprintf(judgeSystemFor(in), in.Today),
		meetingUpdateJudgeUser(in), meetingUpdateJudgeSchema, &out)
	return out, err
}

// meetingUpdateJudgeUser lays out the one meeting, the one claim, and the
// short window of real chat it must be checked against.
// judgeSystemFor picks the question. A "settled in chat" claim has its own.
func judgeSystemFor(in extract.UpdateClaim) string {
	if in.Settled {
		return meetingSettledJudgeSystem
	}
	return meetingUpdateJudgeSystem
}

func meetingUpdateJudgeUser(in extract.UpdateClaim) string {
	var b strings.Builder
	b.WriteString("THE MEETING: " + in.Known.Title)
	if in.Known.Purpose != "" {
		b.WriteString(" — " + in.Known.Purpose)
	}
	b.WriteString("\ncurrently: " + in.Known.When + ", status " + in.Known.Status + "\n")
	// Round 4, "SAME MEETING?": the judge is told who this meeting is with,
	// not only what it is about, so "shared people is not enough" has
	// something to check against.
	if len(in.Known.Attendees) > 0 {
		b.WriteString("attendees: " + strings.Join(in.Known.Attendees, ", ") + "\n")
	}
	b.WriteString("\nTHE CLAIM:\n" + in.Claim + "\n\n")
	b.WriteString("THE MESSAGES:\n\n" + in.Evidence)
	return b.String()
}

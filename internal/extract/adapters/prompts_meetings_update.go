package adapters

// The "what changed?" prompt — the second question asked about a meeting,
// once the first question (prompts_meetings.go) has already found it.
//
// The model is given a numbered list of KNOWN MEETINGS and a later slice of
// the same chat, and answers only what that slice changes about them. Most
// slices change nothing at all, so an empty answer is the normal and correct
// result, not a failure.

const meetingUpdateSystem = `You read one slice of a WhatsApp conversation and
say what it changes about a list of MEETINGS THAT ARE ALREADY KNOWN.

%s
Today is %s.

You will be given the KNOWN MEETINGS, each numbered ("ref"), and then a later
slice of the same chat. Report only what THIS SLICE changes about THOSE
meetings. Most slices change nothing at all — an empty list is the usual and
correct answer.

WHAT COUNTS AS A CHANGE
- The time or day moved: "خليها بكرة", "نأجلها للخميس", "الليلة بدل العصر".
- Postponed with NO new day named: "نأجلها", "مش حقدر اليوم" from somebody who
  is needed for it. Report postponed: true and status "proposed". Do not
  guess a new day that was not said.
- A time agreed by both sides, where none was agreed before: status
  "confirmed".
- Called off: "الغينا الاجتماع", "خلص مش لازم" -> status "cancelled".
- It took place: "الاجتماع كان ممتاز", "زي ما اتفقنا في الاجتماع", or a recap
  of what was decided in it -> status "held".
- SETTLED IN CHAT, with no meeting needed any more: the meeting was proposed
  to decide or discuss something, and the later messages already decided it —
  status "resolved". Example, in spirit: the meeting is "discuss whether to
  join a group and pay a fee". Later one person asks "نعملها skip ولا
  نجازف؟" and another actually answers "انا شايف انو لازم نجازف" — that is a
  real decision, so status "resolved", note "They agreed in chat to take the
  risk and join." A "resolved" meeting ALWAYS needs a note saying what was
  decided — never leave the note empty for this status. It needs a real
  answer from the other side, not just the question being asked again with no
  reply.
- A new place, or a new join link.
- New agenda points — ONLY when somebody in the chat says a point is for THAT
  meeting. In this kind of chat, people mark an agenda point with words such
  as "نناقش في الاجتماع كذا", "ضيف على الأجندة", "خلينا نحكي فيها لما نجتمع" —
  these are SIGNALS TO LOOK FOR in the real messages below, never words to
  copy into your answer. Talk about the same project or topic, with no
  mention of the meeting, is NOT an agenda point. Never copy words from these
  instructions into your answer.

WHAT DOES NOT COUNT
- A DIFFERENT meeting. A second, new meeting belongs to the meeting finder,
  not here — never bend a known meeting to fit one that does not match it.
- General chat about the same topic that does not move, confirm, cancel, or
  end the meeting.
- Somebody's own calendar, mentioned in passing.
- A call starting now or in a few minutes: "يلا بانتظارك", "١٠ دقايق وبندخل",
  "الحين", "join now", "دخلت". That is a separate, ad-hoc call, not a change
  to a meeting that was planned for later — even if the same two people are
  talking.
- A line marked [context].

MATCHING A MESSAGE TO A KNOWN MEETING
When a message could be about more than one known meeting, pick the one whose
title and purpose actually fit it. If you are not sure which one it is about,
leave it out rather than guess. The same two people arrange many meetings —
sharing the people is not enough. The message must be about THIS meeting's
own subject (its title and purpose), or name it, not just involve the same
attendees.

FOR EACH CHANGE
- ref: the number of the known meeting this change is about.
- status: "proposed", "confirmed", "cancelled", "held", or "resolved" when
  the status changed; "" when it did not. "resolved" MUST come with a note —
  see above.
- when_text / time_text: the words as written, exactly like a meeting is
  first reported ("بكرة", "٥ العصر"). NEVER compute a date — that is not your
  job. "" when the timing did not change. Do not repeat the same word in both
  fields — if the timing is only a day-part like "الليلة", put it in
  when_text and leave time_text "".
- postponed: true only when it moved with no new day named at all.
- mode, location, link: only when the chat gives a NEW one; "" otherwise.
- agenda_add: at most 3 new points, only when somebody said a point is for
  THAT meeting (see above), in their own words. Empty is fine, and usual.
- note: one short sentence, in the language the chat is written in, saying
  what happened.
- evidence_id: the #id of the ONE message that shows this change, copied
  exactly from the line it appears on.
- evidence: the words of that message ONLY, copied verbatim. No "[#id]"
  prefix, no timestamp, no speaker name — those belong to the layout, not to
  what was said.
- confidence, and SPREAD THE RANGE:
    0.9  something explicit and unambiguous: "الغينا", a new time both sides
         agreed to
    0.7  clear, but only one side has said it
    0.5  a hint, not a clear statement
  Below 0.6, leave it out entirely. A change to the TIME OR DAY specifically
  needs at least 0.7 — a guessed date is worse than a missed one.

RULES
- Lines marked [context] are there so replies make sense. Read them. Never
  report a change whose only evidence is a context line.
- One entry per known meeting per change. If a meeting is discussed across
  several messages, report it once with the clearest message as evidence.
- When unsure whether something is a real change, leave it out. A missed
  change is simply read again next time. A wrong one rewrites a meeting on a
  guess.
- An empty list is the right answer for most slices of chat.`

// The second opinion on an update — a DIFFERENT question from
// meetingUpdateSystem above, the same trade judgeSystem (prompts.go) makes
// for tasks. The updater is asked "what changed"; this is asked "is that
// claim really what these messages say", which rewards saying no instead of
// finding a change.
//
// Round 3 exists because code alone could not catch this: the evidence read
// "لما يجربو الجمعة" (when the team TESTS on Friday) and the updater moved a
// meeting to Friday. The words were real, the day was real, but they were
// never about the meeting's own time.
const meetingUpdateJudgeSystem = `Here is one planned meeting, a few chat
messages, and one claim about that meeting. Another reader already decided the
claim is right. Your job is narrower: look for ONE OF THE SPECIFIC MISTAKES
below. If none of them is present, the claim stands.

Today is %s.

SAME MEETING? Check this FIRST, before the numbered mistakes. The same two
people arrange many meetings. Sharing the people is not enough. The messages
must be about THIS meeting's subject (its title and purpose) or name it. If
they are about another subject, another client or another project, answer
NO — this is WRONG MEETING (mistake 2) below.

THE MISTAKES. Answer NO only when you can point at one of these:
1. WRONG SUBJECT. The time words are about something else, not about when the
   people in this chat meet: a deadline, when somebody TESTS or TRAVELS or
   delivers, another event. "لما يجربو الجمعة" is when they will TEST on
   Friday — it does not move a meeting to Friday.
2. WRONG MEETING. The messages arrange a different meeting with other people
   or about another subject, or an ad hoc call starting right now.
3. ONLY A QUESTION. Somebody asks or wishes and nobody answers: "شو رايك
   نأجلها؟" with no reply decides nothing.
4. IN PASSING. A long message or voice note mentions a day once, with nothing
   tying that day to this meeting.
5. A LINK WITH NO TIE. A join link posted with no words tying it to this
   meeting is a call starting now, not this meeting's room.

THIS IS HOW AGREEMENT SOUNDS in this kind of chat. These are NOT mistakes:
- Short, informal acceptance: "خلص", "حاضر", "تمام", "ماشي", "ان شاء الله",
  "أكيد". "خلص حاضر الليلة بنحكي" AGREES to talk tonight.
- "الليلة" is tonight and "المسا" is this evening. A meeting first set for
  "اليوم بعد العصر" and then answered with "الليلة بنحكي" HAS moved to tonight;
  "today" and "tonight" do not contradict each other.
- A talk verb is a meeting here: "نحكي", "نتكلم", "نقعد" mean the call or
  sit-down itself.

Do not ask for more proof than a person in this chat would need. Do not answer
NO because something is "not explicit" when the plain reading is clear.

Answer JSON: {"agrees": true or false, "reason": "one short sentence; when
false, name which mistake (1-4) and why"}.`

// The "settled in chat" question, on its own. It is asked the positive way
// round — find the question, find the answer — because a checklist of
// mistakes about time words kept being applied to a claim with no time in it.
const meetingSettledJudgeSystem = `A meeting was planned so that two or more
people could decide or discuss something. Sometimes they then settle it right
there in the chat, and the meeting is no longer needed.

Today is %s.

You get the meeting and a few chat messages. Do two things:
1. From the meeting's title and purpose, say what QUESTION or DECISION it was
   for.
2. Look in the messages for an ANSWER to it from the other person.

Answer agrees=true when the messages hold such an answer. It does not need
formal words. One side asks for a choice ("اعمل skip ولا نجازف؟") and the other
gives their view ("انا شايف انو لازم نجازف"): that is an answer, and it stays
an answer when it comes with a complaint, a joke, doubt about money, or
swearing.

Answer agrees=false only when:
- the question is asked and nobody answers it, or
- the reply is about a different subject, or
- the reply puts the decision off ("خلينا نحكي فيها لما نقعد", "بنشوف بعدين").

Answer JSON: {"agrees": true or false, "reason": "one short sentence: the
question, and the answer you found or why there is none"}.`

var meetingUpdateJudgeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"agrees": map[string]any{"type": "boolean"},
		"reason": map[string]any{"type": "string"},
	},
	"required": []string{"agrees", "reason"},
}

var meetingUpdateSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"updates": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref":       map[string]any{"type": "integer"},
					"status":    map[string]any{"type": "string"},
					"when_text": map[string]any{"type": "string"},
					"time_text": map[string]any{"type": "string"},
					"postponed": map[string]any{"type": "boolean"},
					"mode":      map[string]any{"type": "string"},
					"location":  map[string]any{"type": "string"},
					"link":      map[string]any{"type": "string"},
					"agenda_add": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
					},
					"note":        map[string]any{"type": "string"},
					"evidence_id": map[string]any{"type": "string"},
					"evidence":    map[string]any{"type": "string"},
					"confidence":  map[string]any{"type": "number"},
				},
				"required": []string{
					"ref", "status", "when_text", "time_text", "postponed",
					"mode", "location", "link", "agenda_add", "note",
					"evidence_id", "evidence", "confidence",
				},
			},
		},
	},
	"required": []string{"updates"},
}

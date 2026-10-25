package adapters

// The meetings prompt.
//
// Almost every line here is about NOT creating a meeting. That is the same
// lesson the agent version learned (agent/extract-meetings.mjs) and the rules
// are carried over from it, because they were written against real chats:
//
//   - a bare join link is not a meeting. "https://meet.google.com/abc-defg-hij"
//     with no plan around it is somebody starting a call now.
//   - "يلا بانتظارك", "تفضل", "انا جاهز" is an ad-hoc call, not an arrangement.
//   - somebody else's calendar is not ours. "عندي اجتماع الساعة ٧" is them
//     telling you why they are busy.
//   - politeness is not a meeting. "نشوفك قريب ان شاء الله" agrees to nothing.
//
// And one rule pushes the other way, because it is the case that matters most:
// a meeting AGREED WITH NO DATE ("لازم نجتمع قريب") is a real meeting and the
// one most likely to be forgotten. It is recorded with no date at all rather
// than skipped or given an invented one.

const meetingSystem = `You read one slice of a WhatsApp conversation and report
the MEETINGS being arranged in it.

%s
Today is %s.

WHAT A MEETING IS
Something ARRANGED IN ADVANCE between people. Not a task with a date, and not
every phone call. Any of these is a strong signal:
- a day or time named before the event: "الخميس الساعة ٥", "بكرة ٤ العصر",
  "Tuesday 2pm"
- a stated purpose: "للتعريف عن المنتج", "لمناقشة العقد", "to go through the plan"
- asking people to confirm or attend: "انتظر تأكيدكم", "يناسبكم؟",
  "ضروري حضور الجميع"
- a place being agreed: "الاجتماع الحضوري في مكتبنا بجاده ٣٠"
- a calendar invite: "has invited you to join a video meeting on Google Meet"
- AGREEING TO MEET WITH NO DATE YET. This counts and must NOT be skipped:
  "نتقابل ونحكي في الموضوع", "لازم نجتمع قريب", "خلينا نعمل اجتماع الأسبوع الجاي",
  "let's set up a call to go through this". Report it with status "proposed",
  when_text set to whatever was said about timing ("قريب", "الأسبوع الجاي") and
  time_text empty. A meeting that is agreed but unscheduled is the one people
  forget, which is exactly why it must be recorded.

WHAT IS NOT A MEETING
- A bare join link with nothing planned around it. That is a call starting now.
- "يلا بانتظارك", "تفضل", "انا جاهز", "join now", "اتصل فيني" — ad-hoc calls.
- Somebody else's meeting, mentioned in passing: "عندي اجتماع الساعة ٧"
  (their own calendar), "اجتماعهم الداخلي". Only report meetings the people in
  THIS chat are arranging with each other.
- A meeting that already happened, mentioned afterwards: "الاجتماع كان ممتاز",
  "التقينا امس". The arranging is over; there is nothing to record.
- Politeness with no subject and no agreement: "نشوفك قريب ان شاء الله",
  "we should catch up sometime", "تعال زورنا". An undated meeting needs BOTH a
  real subject AND an actual agreement — somebody proposed meeting about
  something and somebody else agreed. Good manners alone are not a meeting.
- Preparing FOR a meeting: "جهز العرض قبل الاجتماع" is a task, not a meeting.
  Ignore it; tasks are handled elsewhere.
- Asking somebody's opinion or advice in the chat ("قلت آخد رأيك", "شو رايك
  اروح ولا لا"), or wondering whether to attend an event run by OTHER people
  (a community's intro session, a conference), is not a meeting between the
  people in THIS chat.

FOR EACH MEETING
- title: short, in the language it was said in. What the meeting is, not when.
- purpose: why they are meeting, in their words, or "" when unsaid.
- evidence_id: the #id of the ONE message that best shows this meeting being
  arranged, copied exactly from the line it appears on.
- evidence: the words of that message ONLY, copied verbatim. No "[#id]" prefix,
  no timestamp, no speaker name — those belong to the layout, not to what was
  said.
- when_text: the day as written ("الخميس", "بكرة", "next week", "قريب"), or ""
  when nobody named one. NEVER compute a date. NEVER invent one.
- time_text: the clock time as written ("٥ العصر", "2pm", "الساعة ٤"), or "".
- status: "confirmed" only when a specific time was actually agreed by more
  than one person. Everything else is "proposed".
- mode: "online", "in_person", or "" when the chat does not say.
- location: the place in their words ("مكتبنا بجاده ٣٠"), including any map
  link that was shared. "" when there is none.
- link: the join link exactly as sent, or "".
- attendees: the people meeting, named as the chat names them. Leave it empty
  when the chat does not say. NEVER invent a name.
- agenda: the points to be covered, one per line, in their words. Empty is fine.
- confidence, and SPREAD THE RANGE:
    0.9+  a calendar invite, or a time clearly agreed by both sides
    0.7   a clear proposal with a day or a purpose
    0.5   they agreed to meet but nothing is settled
    0.3   might be a meeting, might be talk
  Below 0.5, leave it out entirely.

RULES
- Lines marked [context] are there so replies make sense. Read them. Never
  report a meeting whose only evidence is a context line.
- One entry per meeting. If a meeting is discussed across eight messages, that
  is ONE meeting, with the clearest message as evidence.
- When unsure about the DATE, still report the meeting and leave when_text and
  time_text as the words that were actually said.
- When unsure whether it is a meeting AT ALL, leave it out. A missed meeting
  costs nothing. A wrong one carries people and a time, and someone has to
  undo it.
- An empty list is the right answer for most slices of chat.`

var meetingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"meetings": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string"},
					"purpose":     map[string]any{"type": "string"},
					"evidence_id": map[string]any{"type": "string"},
					"evidence":    map[string]any{"type": "string"},
					"when_text":   map[string]any{"type": "string"},
					"time_text":   map[string]any{"type": "string"},
					"status":      map[string]any{"type": "string"},
					"mode":        map[string]any{"type": "string"},
					"location":    map[string]any{"type": "string"},
					"link":        map[string]any{"type": "string"},
					"attendees": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
					},
					"agenda": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
					},
					"confidence": map[string]any{"type": "number"},
				},
				"required": []string{
					"title", "purpose", "evidence_id", "evidence", "when_text",
					"time_text", "status", "mode", "location", "link",
					"attendees", "agenda", "confidence",
				},
			},
		},
	},
	"required": []string{"meetings"},
}

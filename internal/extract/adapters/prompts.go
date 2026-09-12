// Package adapters holds the model-facing implementations of extract.Extractor.
//
// Everything here is replaceable. Nothing here may open the database or call
// the bridge API — an adapter receives text and returns JSON, and the pipeline
// checks the answer.
package adapters

// The prompts are shared by every adapter so that switching engines compares
// models, not wording.
//
// Each rule below exists because a model broke it in testing on 2026-09-12:
//
//   - past tense: "التقيت مع فلان" and "نقلت التقرير" were returned as tasks.
//     They are status updates. The work is finished.
//   - meetings: "الاجتماع القادم الخميس" was returned as a task. Arranging a
//     meeting is not doing work, and meetings have their own module.
//   - invented owners: every model tried to name somebody. Owners are resolved
//     in code from mentions and replies; the model's guess is a hint at most.
//   - evidence: without it nothing is checkable. With it, precision jumped and
//     hallucinations became detectable rather than plausible.
//   - padding: one model returned the same task four ways. An empty list has
//     to be an acceptable answer or the model will always find something.
//
// One rule was REMOVED on 2026-09-12 after measuring against hand-labelled
// chats: "a bug report is not a task". It cost more than half the recall on
// client chats, where "the map needs changing" and "the report comes out
// empty" are precisely the work. A defect reported to the people who own the
// thing is a request, whatever mood it is written in.

const extractSystem = `You read one slice of a WhatsApp work conversation and
report the work it contains.

%s
WHAT COUNTS AS A TASK
Work that still has to be DONE by somebody.
- a request: "جهز العقد", "ابعتلي الاكسز", "please review the contract"
- a commitment: "انا هعملها", "I'll prepare it", "بعملها بكرة"
- an instruction that creates work: "الاهداف لازم يتم ادخالها في FlowOS"
- a change asked for: "نغير الخريطة", "جميل لو قدرنا نضيف اسم الدولة",
  "نحتاج نضيف قياس للتغطية", "we need the country name next to each item"
- a defect reported to the people who own the thing: "الشات ما بيحدث الا لما
  اعمل refresh", "التقرير بيطلع فاضي", "موضوع الفراغات ما تم علاجه". In a
  client or support chat this IS the work — somebody has to fix it.

WHAT DOES NOT COUNT
- Anything already finished. Past tense is a status update, not a task.
  "التقيت مع فلان", "نقلت التقرير", "عملت شوية تعديلات", "I held the meeting",
  "sent it yesterday" are NOT tasks. The work is over.
- A question. "ممكن مثال؟", "شو رايكم؟", "ليش ما بيضل فاتح؟", "what do you
  think?" are questions. Asking is not assigning.
- Grumbling about something nobody in this chat owns: a slow airline, a
  competitor's app, the weather. Nobody here can act on it.
- A meeting being arranged. "نجتمع الخميس" is a meeting, not a task. Meetings
  are handled elsewhere. Ignore them completely.
- Opinions, greetings, thanks, jokes, forwarded articles, general discussion.
- Work you can only infer. If nobody asked and nobody committed, there is no
  task.

FOR EACH TASK
- title: short and concrete, in the language it was said in.
- owner_text: the person who must DO it, named as the chat names them. Use
  "unknown" when the chat does not say. NEVER invent a name, and never guess
  from context — an unknown owner is a fine answer.
- evidence_id: the #id of the ONE message that proves this task exists, copied
  exactly from the line it appears on.
- evidence: the words of the message ONLY, copied verbatim. Do not paraphrase,
  translate, correct or shorten them, and do not include the "[#id]" prefix,
  the timestamp, or the speaker's name — those belong to the layout, not to
  what was said.
- due_text: the timing words as written ("بكرة", "الخميس", "next week"), or ""
  when none were said. Never compute a date.
- priority_hint: low, normal or high.
- confidence: how sure you are, and SPREAD THE RANGE. Returning 1.0 for
  everything tells the reader nothing.
    1.0  someone was named and told to do a specific thing
    0.8  a clear request or commitment, owner obvious from context
    0.6  reads like work, but the wording is indirect
    0.4  might be a task, might be discussion
  Below 0.4, leave it out entirely.

RULES
- Lines marked [context] are there so replies make sense. Read them. Never
  draw a task from one.
- One task per piece of work. If the same work is discussed in five messages,
  that is one task, with the clearest message as evidence.
- Return only tasks you can point at real words for.
- Before returning a task, re-read your evidence and ask: does somebody have
  to DO something because of these words? A bare question and a report of
  finished work do not pass. A defect or a change named to the people who own
  the thing does.
- Read the slice from the first line to the last, and report EVERY piece of
  work you find. A busy slice can hold ten or more; a quiet one holds none.
  Missing a real request is as wrong as inventing one.
- An empty list is a good answer when the slice is small talk. Do not pad, and
  do not repeat one piece of work in several shapes.`

// The judge is deliberately a different question from the extractor's. The
// extractor is asked "what work is here", which rewards finding things. The
// judge is asked "would this belong on somebody's task list", which rewards
// saying no. Same model, opposite pressure.
const judgeSystem = `You are checking a list of tasks somebody pulled out of a
WhatsApp conversation. For each one, say whether it belongs on a work task
list.

Answer YES when the words make somebody responsible for doing something:
a request, a change asked for, a defect reported to the people who own it, a
promise to do work.

Answer NO when it is:
- talk about arranging, confirming or attending a meeting
- something happening right now, finished within minutes: "send me the pin",
  "I'm on my way", "tell me when you get in", "come down"
- a courtesy: "say hi to him", "I'm around if you need anything",
  "let me know how he is"
- an example, not an instruction: somebody listing the kinds of question you
  could ask a system is describing it, not assigning work
- a description of how something already works, or a plan still being argued
  about with no decision
- a refusal, or a reason something cannot be done
- already finished

Ask yourself one question: a week from now, would somebody still owe this? If
it is done and gone within the hour, the answer is NO.

Return one verdict per task, with its index. Give a short reason for every NO.`

// A softer version of the two paragraphs above was tried and measured: it
// added "size is not the test", to stop the judge dropping small real tasks
// like "send me the access". It let more chatter through without recovering
// any of the tasks, and precision fell from 0.67 to 0.56 at identical recall.
// The strict wording is kept because the numbers preferred it.

const completionSystem = `You read one slice of a WhatsApp conversation and
report which of the listed open tasks it says are FINISHED.

A task is finished when someone states the work is done: "تم", "خلصت",
"انجزته", "done", "sent it", "رفعته". A promise to do it later is not a
completion. A question about it is not a completion.

FOR EACH COMPLETION
- task_id: the numeric id from the list.
- evidence_id: the #id of the message that says so, copied exactly.
- evidence: the words from that message, copied verbatim.
- confidence: 0.0 to 1.0.

Return an empty list when nothing was finished. Never report a completion for
a task that is not in the list.`

// extractSchema and completionSchema are JSON Schema documents. Ollama
// enforces them; the Claude sidecar is told to obey them. Every field is
// required — an optional field is one a model will quietly drop.
var extractSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"tasks": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":         map[string]any{"type": "string"},
					"owner_text":    map[string]any{"type": "string"},
					"evidence_id":   map[string]any{"type": "string"},
					"evidence":      map[string]any{"type": "string"},
					"due_text":      map[string]any{"type": "string"},
					"priority_hint": map[string]any{"type": "string"},
					"confidence":    map[string]any{"type": "number"},
				},
				"required": []string{
					"title", "owner_text", "evidence_id", "evidence",
					"due_text", "priority_hint", "confidence",
				},
			},
		},
	},
	"required": []string{"tasks"},
}

var judgeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"verdicts": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index":   map[string]any{"type": "integer"},
					"is_task": map[string]any{"type": "boolean"},
					"reason":  map[string]any{"type": "string"},
				},
				"required": []string{"index", "is_task", "reason"},
			},
		},
	},
	"required": []string{"verdicts"},
}

var completionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"completions": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":     map[string]any{"type": "integer"},
					"evidence_id": map[string]any{"type": "string"},
					"evidence":    map[string]any{"type": "string"},
					"confidence":  map[string]any{"type": "number"},
				},
				"required": []string{"task_id", "evidence_id", "evidence", "confidence"},
			},
		},
	},
	"required": []string{"completions"},
}

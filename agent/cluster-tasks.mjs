// Claude Agent SDK sidecar: cluster a circle's open tasks into a 2-level
// parent/child hierarchy. Runs right after extraction and from the manual
// "Cluster tasks" button.
//
// Single-shot LLM call (no tools, no MCP). Input on stdin is the task list;
// the bridge applies the proposed parents.
//
// Auth: uses the local Claude subscription (Max). Do NOT set ANTHROPIC_API_KEY.
//
// stdin:  JSON { circle_name, circle_description, tasks: [{id, title,
//           description, priority, status, assignee}] }
// stdout (last line): { ok, clusters: [...] }

import { query } from '@anthropic-ai/claude-agent-sdk'

function readStdin() {
  return new Promise((resolve) => {
    let data = ''
    process.stdin.setEncoding('utf8')
    process.stdin.on('data', (c) => (data += c))
    process.stdin.on('end', () => resolve(data))
    if (process.stdin.isTTY) resolve('')
  })
}

const raw = (await readStdin()).trim()
let input
try {
  input = JSON.parse(raw)
} catch {
  process.stdout.write(JSON.stringify({ ok: false, clusters: [] }) + '\n')
  process.exit(1)
}

const tasks = Array.isArray(input.tasks) ? input.tasks : []
if (tasks.length < 2) {
  // Nothing to cluster.
  process.stdout.write(JSON.stringify({ ok: true, clusters: [] }) + '\n')
  process.exit(0)
}

const systemPrompt = `You are a task organizer. You receive a flat list of TASKS extracted from WhatsApp messages, all belonging to the same Circle. Your job is to identify groups of related tasks that share a single concrete goal, and propose a parent task for each group so the user sees structure instead of a flat list.

WHAT COUNTS AS A CLUSTER (≥2 tasks):
- Same PROJECT or DELIVERABLE (e.g. "the PlayBook", "Salla MCP Studio MVP", "Mahmoud Atef client engagement")
- Same DECISION being worked toward (e.g. "the Q4 hiring round", "rollout timing for X")
- Same EXTERNAL PERSON whose engagement is the unifying goal (e.g. "everything around onboarding Salman")
- A clear UMBRELLA + SUBTASKS pattern (one big outcome and its prerequisites)

WHAT IS *NOT* A CLUSTER:
- Same assignee but unrelated subjects ("Mohammed has 5 tasks" doesn't cluster them)
- Same broad topic with no shared deliverable ("hiring tasks" no; "hiring for the PE role" yes)
- Tasks that happen to mention each other in passing

FOR EACH CLUSTER, CHOOSE ONE:
A) parent_existing_id — when ONE of the existing tasks IS the umbrella (the most outcome-level / general one). Reuse it as the parent; the others become its children.
B) parent_new — when no single task is umbrella-like. Propose a NEW parent task with a concise title (≤80 chars) describing the shared outcome, and a 1-sentence description.

RULES:
- A task belongs to AT MOST ONE parent.
- No chains: a parent must NOT itself be a child of another parent in your output.
- Be conservative. Leaving a task standalone is fine. A weak cluster is worse than no cluster.
- Don't reshape or merge — only group. Don't invent extra tasks beyond new parents.
- Match the language of the existing tasks (English / Arabic / mix) when proposing parent titles.

OUTPUT ONLY strict JSON, no markdown:
{
  "clusters": [
    {"parent_existing_id": <id>, "child_ids": [<id>,...], "rationale": "..."},
    {"parent_new": {"title": "...", "description": "..."}, "child_ids": [<id>,...], "rationale": "..."}
  ]
}
If there are no good clusters, output {"clusters": []}.`

const prompt = `Circle: ${input.circle_name || '(unknown)'}
Purpose: ${input.circle_description || '(no profile yet)'}

Tasks (${tasks.length}):
${tasks
  .map(
    (t) =>
      `#${t.id} [${t.priority || 'normal'}${t.status && t.status !== 'open' ? '/' + t.status : ''}]` +
      (t.assignee ? ` @${t.assignee}` : '') +
      ` — ${t.title}` +
      (t.description ? `\n   ${String(t.description).slice(0, 200)}` : ''),
  )
  .join('\n')}

Cluster them now. Output strict JSON.`

let body = ''
let isError = false
try {
  const response = query({
    prompt,
    options: { systemPrompt, maxTurns: 1, allowedTools: [] },
  })
  for await (const msg of response) {
    if (msg.type === 'assistant') {
      for (const block of msg.message?.content || []) {
        if (block.type === 'text' && block.text) body += block.text
      }
    } else if (msg.type === 'result') {
      if (msg.result && !body) body = msg.result
      isError = !!msg.is_error
    }
  }
} catch (e) {
  isError = true
  body = String(e?.message || e)
}

body = body.trim()
const fenced = body.match(/```(?:json)?\s*([\s\S]+?)\s*```/i)
if (fenced) body = fenced[1].trim()

let parsed
try {
  parsed = JSON.parse(body)
} catch {
  parsed = { clusters: [] }
}

process.stdout.write(
  JSON.stringify({
    ok: !isError,
    clusters: Array.isArray(parsed.clusters) ? parsed.clusters : [],
  }) + '\n',
)
process.exit(isError ? 1 : 0)

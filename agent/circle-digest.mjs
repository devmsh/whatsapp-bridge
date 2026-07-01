// Claude Agent SDK sidecar: write the narrative pieces of one circle's
// incremental digest.
//
// Adapted from briefing.mjs, but scoped to a single circle and INCREMENTAL:
// the bridge gathers the raw facts for one circle's flattened chats
// (watermark-floored, not a fixed 24h window) and pipes them on stdin along
// with the previous digest's summary. This sidecar's only job is to write:
//   - a 1-2 sentence top-line for the circle
//   - one short narrative per signal chat
//
// Single-shot, no tools, no MCP. Auth via the user's Claude subscription
// (CLAUDE_CODE_OAUTH_TOKEN); ANTHROPIC_API_KEY must be unset.
//
// stdin:  JSON { circle_name, previous_summary, new_message_count, tasks_top,
//                tasks_overdue, awaiting_reply, signal_chats }
// stdout (last line): { ok, summary, signal_summaries: { <chat_jid>: "..." } }

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
  process.stdout.write(JSON.stringify({ ok: false, summary: 'invalid input' }) + '\n')
  process.exit(1)
}

const circleName = input.circle_name || 'this circle'
const hasPrevious = !!(input.previous_summary && String(input.previous_summary).trim())

const systemPrompt = `You write a focused digest for one WhatsApp circle called "${circleName}" for a busy founder. You will be given structured facts scoped to this circle only (tasks, overdue items, signal chats with recent messages, chats awaiting reply). Your job:

1. Write a 1-2 sentence TOP-LINE that captures what matters right now in "${circleName}". Calm, specific, no fluff. Mention concrete names/topics, not "you have items".
2. For each SIGNAL CHAT, write ONE short sentence (≤ 25 words) describing what is happening there RIGHT NOW based on the sampled messages. Note: a decision was made / a question is open / someone delivered / someone is blocked. No greetings, no padding.

${hasPrevious
    ? 'IMPORTANT: `previous_summary` is non-empty, so you are UPDATING an existing digest, not writing from scratch: keep what is still accurate, drop what is now stale, and weave in what is new from the supplied signal chats — produce one coherent, current summary, not an appended log.'
    : 'There is no previous digest for this circle yet — write the top-line from scratch.'}

Output strict JSON only, exactly this shape (no markdown, no commentary):
{"summary":"…","signal_summaries":{"<chat_jid>":"…","<chat_jid>":"…"}}

Rules:
- Keys in signal_summaries MUST match the chat_jid values you were given. Skip any chat where the sample is too thin.
- Never invent facts. If the sample doesn't show clear activity, say "Quiet in ${circleName}."`

const prompt = `Here is the circle digest data for "${circleName}". Write the top-line and per-chat narratives.\n\n${JSON.stringify(input, null, 2)}`

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
// The model is asked for raw JSON, but it sometimes wraps in ```json …```. Strip.
const fenced = body.match(/```(?:json)?\s*([\s\S]+?)\s*```/i)
if (fenced) body = fenced[1].trim()

let parsed
try {
  parsed = JSON.parse(body)
} catch {
  parsed = { summary: body || '(no summary)', signal_summaries: {} }
}

process.stdout.write(
  JSON.stringify({
    ok: !isError,
    summary: parsed.summary || '',
    signal_summaries: parsed.signal_summaries || {},
  }) + '\n',
)
process.exit(isError ? 1 : 0)

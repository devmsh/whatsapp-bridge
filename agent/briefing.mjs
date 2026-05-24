// Claude Agent SDK sidecar: write the narrative pieces of a daily briefing.
//
// The bridge gathers all the raw facts (tasks, overdue, signal chats with a
// sample of messages, awaiting-reply chats) and pipes them on stdin. This
// sidecar's only job is to write:
//   - a 1-2 sentence top-line
//   - one short narrative per signal chat
//
// Single-shot, no tools, no MCP. Auth via the user's Claude subscription
// (CLAUDE_CODE_OAUTH_TOKEN); ANTHROPIC_API_KEY must be unset.
//
// stdin:  JSON { date, tasks_top, tasks_overdue, awaiting_reply, signal_chats }
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

const systemPrompt = `You write a daily WhatsApp briefing for a busy founder. You will be given structured facts (tasks, overdue items, signal chats with recent messages, chats awaiting reply). Your job:

1. Write a 1-2 sentence TOP-LINE that captures what matters today. Calm, specific, no fluff. Mention concrete names/topics, not "you have items".
2. For each SIGNAL CHAT, write ONE short sentence (≤ 25 words) describing what is happening there RIGHT NOW based on the sampled messages. Note: a decision was made / a question is open / someone delivered / someone is blocked. No greetings, no padding.

Output strict JSON only, exactly this shape (no markdown, no commentary):
{"summary":"…","signal_summaries":{"<chat_jid>":"…","<chat_jid>":"…"}}

Rules:
- Keys in signal_summaries MUST match the chat_jid values you were given. Skip any chat where the sample is too thin.
- If a signal chat is the user's own broadcast/personal thread, just say so briefly.
- Never invent facts. If the sample doesn't show clear activity, say "Quiet today."`

const prompt = `Here is today's briefing data. Write the top-line and per-chat narratives.\n\n${JSON.stringify(input, null, 2)}`

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

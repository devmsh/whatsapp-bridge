// Claude Agent SDK sidecar: draft 2-3 candidate replies for one chat.
//
// The bridge gathers recent messages + the chat's profile + a few of the user's
// previous outgoing messages (tone samples) and pipes them on stdin. This
// sidecar runs a single LLM call and returns candidate replies.
//
// Auth via Claude subscription (CLAUDE_CODE_OAUTH_TOKEN); no ANTHROPIC_API_KEY.
//
// stdin:  JSON { kind:'group'|'contact', chat_label, profile, recent_messages, my_recent_tone, locale_hint }
// stdout (last line): { ok, drafts: [{ text, style, reason }] }

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
  process.stdout.write(JSON.stringify({ ok: false, drafts: [] }) + '\n')
  process.exit(1)
}

const kindWord = input.kind === 'group' ? 'WhatsApp GROUP' : 'one-to-one chat (DM)'

const systemPrompt = `You draft short candidate replies the user can send in a ${kindWord}.

You will receive:
- The chat's PURPOSE profile (one paragraph about what this conversation is about).
- The RECENT MESSAGES in chronological order ("Me" means the user). Pay attention to the LATEST messages — that's what the user is responding to.
- TONE SAMPLES: a few of the user's previous outgoing messages in this chat (match this voice — formality, length, language, emoji habits).

Produce 2 OR 3 candidate replies that the user could send NOW. Each one should:
- Address what was actually said in the last few messages.
- Match the user's tone shown in the samples (language, length, register).
- Differ from each other meaningfully — one might be concise, one warmer, one a question, etc.
- Be ready-to-send: no placeholders like "[Name]", no markdown, just the message text.
- Use the user's language. If the chat is in Arabic, write in Arabic. If mixed, follow the dominant language of the user's tone samples.

Output STRICT JSON only, exactly this shape (no commentary, no markdown):
{"drafts":[{"text":"…","style":"concise","reason":"one short phrase"},{"text":"…","style":"warm","reason":"one short phrase"}]}

If there is nothing to reply to (e.g. last message is from Me, or the chat has no recent content), output {"drafts":[]}.`

const prompt = `Draft replies for this ${kindWord}.

CHAT: ${input.chat_label || '(unknown)'}

PROFILE (purpose):
${input.profile || '(no profile yet)'}

RECENT MESSAGES (oldest→newest):
${(input.recent_messages || []).join('\n')}

YOUR TONE SAMPLES (a few of the user's previous outgoing messages here):
${(input.my_recent_tone || []).join('\n') || '(none — match a neutral, professional, concise voice)'}

Now produce the JSON.`

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
  parsed = { drafts: [] }
}

process.stdout.write(
  JSON.stringify({ ok: !isError, drafts: Array.isArray(parsed.drafts) ? parsed.drafts : [] }) + '\n',
)
process.exit(isError ? 1 : 0)

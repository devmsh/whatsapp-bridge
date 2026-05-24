// Claude Agent SDK sidecar: write a short "purpose" profile for one entity
// (a WhatsApp group, a DM contact, or a Circle).
//
// Single-shot summarizer — no tools, no MCP. The bridge assembles the context
// (a message sample, or member profiles for a circle) and pipes it on stdin.
//
// Auth: uses the local Claude subscription — do NOT set ANTHROPIC_API_KEY.
//
// Usage:  node profile.mjs <kind>      (kind: group | contact | circle)
//   stdin: the context text to summarize
//   stdout (last line): {"ok":true,"description":"..."}

import { query } from '@anthropic-ai/claude-agent-sdk'

const kind = process.argv[2] || 'group'

function readStdin() {
  return new Promise((resolve) => {
    let data = ''
    process.stdin.setEncoding('utf8')
    process.stdin.on('data', (c) => (data += c))
    process.stdin.on('end', () => resolve(data))
    if (process.stdin.isTTY) resolve('')
  })
}

const context = (await readStdin()).trim()

const kindWord = kind === 'circle' ? 'Circle (a user-defined cluster of chats)' : kind === 'contact' ? 'one-to-one chat (DM)' : 'WhatsApp group'

const systemPrompt = `You write a short, factual PURPOSE PROFILE for a ${kindWord}.

Goal: a profile that helps a task-tracking agent later understand what this ${kind} is about, who the key people are, and what kind of work or topics happen here — so it can tell apart discussions that belong here from unrelated ones.

Write 2-5 sentences (or short bullet-like sentences). Cover, when the content shows it:
- The main topic / purpose (project, company, deal, family, vendor, etc.).
- Recurring themes or workstreams.
- Key people and their apparent role (only if clear).
- Language(s) used (e.g. Arabic, English, mixed).
Be concrete and specific to THIS content. Do NOT invent facts. Do NOT add greetings, headings, or meta-comments. If the content is too thin to say anything useful, reply with exactly: INSUFFICIENT.

Output ONLY the profile text (or INSUFFICIENT). No preamble.`

const prompt = `Here is the context for the ${kind}. Write its purpose profile.\n\n${context}`

let description = ''
let isError = false
try {
  const response = query({
    prompt,
    options: { systemPrompt, maxTurns: 1, allowedTools: [] },
  })
  for await (const msg of response) {
    if (msg.type === 'assistant') {
      for (const block of msg.message?.content || []) {
        if (block.type === 'text' && block.text) description += block.text
      }
    } else if (msg.type === 'result') {
      if (msg.result && !description) description = msg.result
      isError = !!msg.is_error
    }
  }
} catch (e) {
  isError = true
  description = String(e?.message || e)
}

description = description.trim()
process.stdout.write(JSON.stringify({ ok: !isError, description }) + '\n')
process.exit(isError ? 1 : 0)

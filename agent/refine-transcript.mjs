// Claude Agent SDK sidecar: refine a raw whisper transcript for one WhatsApp
// voice note, using recent chat context.
//
// Whisper gives a near-verbatim Arabic transcript that:
//   - has no punctuation (single run-on sentence),
//   - sometimes mishears words on tech jargon,
//   - transliterates English terms to Arabic spelling (e.g. "إم سي بي" instead
//     of "MCP"), or vice versa.
// We post-process with an LLM to: keep the speaker's words, but add
// punctuation, restore the right English spelling for tech terms, and
// optionally use markdown bullets when the speaker naturally listed things.
//
// Single-shot, no MCP, no tools. Auth via Claude subscription.
//
// stdin (JSON):
//   {
//     "raw":             "<whisper output>",
//     "recent_messages": ["Alaa: …", "Me: …", …]   // optional, up to 10
//   }
//
// stdout (last line):
//   { "ok": true, "refined": "<cleaned text>" }

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
  process.stdout.write(JSON.stringify({ ok: false, refined: '' }) + '\n')
  process.exit(1)
}

const rawText = String(input.raw || '').trim()
if (!rawText) {
  process.stdout.write(JSON.stringify({ ok: true, refined: '' }) + '\n')
  process.exit(0)
}

const recent = Array.isArray(input.recent_messages) ? input.recent_messages : []

const systemPrompt = `You are a transcript cleaner for Arabic WhatsApp voice notes.

INPUT
- "RAW TRANSCRIPT": the verbatim output of whisper on one voice note. It is mostly correct but has zero punctuation, may have a few misheard words on technical terms, and sometimes transliterates English words into Arabic letters (e.g. writes "إم سي بي" or "هب سبوت" instead of "MCP" or "HubSpot").
- "CONTEXT": the most recent messages in the same chat (oldest→newest). Use this ONLY to disambiguate names, product names, tech terms, and topics that appear in the voice note. Do not invent content that is not in the raw transcript.

YOUR OUTPUT (the refined transcript)
1. Keep the speaker's words faithfully. Do NOT paraphrase, summarize, or add anything they did not say. Only fix obvious mishearings using the chat context.
2. Add proper punctuation where the speaker paused or finished a thought:
   - "،" or "." after a clause
   - "؟" or "?" for questions (whichever matches the surrounding language)
   - "!" for exclamations
   Break the text into normal sentences and short paragraphs based on the natural rhythm.
3. Restore English spelling for any English word, brand, product, or acronym the speaker said in English — even if whisper transliterated it to Arabic letters. Examples: "إم سي بي" → "MCP", "هب سبوت" → "HubSpot", "أوديتور" → "auditor", "كونتاكتس" → "contacts", "سينكرونيزيشن" → "synchronization". Match what the speaker actually said in English; do not translate Arabic words to English.
4. Keep Arabic words in Arabic. Keep English words in English. The output is mixed AR/EN, mirroring what was spoken.
5. If the speaker naturally enumerates (talks about "first this, second that…", lists items, or counts off steps), format that part as a markdown bulleted or numbered list. Otherwise keep prose.
6. Do not add headings. Do not add a summary at the end. Do not invent speaker labels. Do not include the original raw text. Do not wrap the output in quotes or code fences.

If you cannot improve the raw transcript (it is already clean, or too short), output it unchanged.

Output ONLY the cleaned transcript text. No JSON, no markdown fences.`

const ctxBlock = recent.length
  ? `CONTEXT (recent chat, oldest→newest):\n${recent.map((l) => '  ' + l).join('\n')}\n\n`
  : ''

const prompt = `${ctxBlock}RAW TRANSCRIPT:\n"""\n${rawText}\n"""\n\nReturn the cleaned transcript text only.`

// One round of refinement. Returns { body, isError }.
async function runOnce() {
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
  return { body, isError }
}

// Two attempts max — if the first hits an "Overloaded" / 5xx, wait 8s and try
// again before falling back to the raw transcript on the bridge side.
let { body, isError } = await runOnce()
const overloaded = isError && /Overloaded|5\d\d|temporarily|try again/i.test(body)
if (overloaded) {
  await new Promise((r) => setTimeout(r, 8000))
  ;({ body, isError } = await runOnce())
}

body = body.trim()
// If the model wrapped in fences, strip them.
const fenced = body.match(/^```(?:\w+)?\s*([\s\S]+?)\s*```$/)
if (fenced) body = fenced[1].trim()

process.stdout.write(
  JSON.stringify({ ok: !isError, refined: body || rawText }) + '\n',
)
process.exit(isError ? 1 : 0)

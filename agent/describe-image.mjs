// Claude Agent SDK sidecar: describe one image with vision.
//
// Reads the image file path as argv[2]. Runs a single-shot LLM call that
// returns a concise description: what's in it, any text visible (OCR), and
// any cue useful for task extraction (e.g. "screenshot of a CRM page", "photo
// of an invoice for…").
//
// Auth via Claude subscription. ANTHROPIC_API_KEY must be unset.
//
// Output (stdout, last line): {"ok":true,"description":"…"}

import { readFile } from 'node:fs/promises'
import { extname } from 'node:path'
import { query } from '@anthropic-ai/claude-agent-sdk'

const path = process.argv[2]
if (!path) {
  process.stdout.write(JSON.stringify({ ok: false, description: 'no path' }) + '\n')
  process.exit(2)
}

let bytes
try {
  bytes = await readFile(path)
} catch (e) {
  process.stdout.write(JSON.stringify({ ok: false, description: 'read failed: ' + (e?.message || e) }) + '\n')
  process.exit(1)
}

const mime = (() => {
  const e = extname(path).toLowerCase()
  if (e === '.png') return 'image/png'
  if (e === '.gif') return 'image/gif'
  if (e === '.webp') return 'image/webp'
  return 'image/jpeg'
})()

const dataURL = `data:${mime};base64,${bytes.toString('base64')}`

const systemPrompt = `You describe a WhatsApp image in 1-3 short sentences. Cover:
- What kind of image it is (screenshot, photo, document, meme, etc.)
- The main subject or any visible text (OCR the readable text)
- Any detail that would help a task-tracking agent understand it (e.g. a receipt for $X, a chart of Y, a UI screenshot of Z)
Be concrete; do not say "this image shows". No greetings, no markdown. If the image is uninformative (blank, decorative), say "Uninformative." in plain text.`

const prompt = [
  { type: 'image', source: { type: 'base64', media_type: mime, data: bytes.toString('base64') } },
  { type: 'text', text: 'Describe this image as instructed.' },
]

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

// Silence unused-vars (dataURL kept for reference / future use).
void dataURL

process.stdout.write(JSON.stringify({ ok: !isError, description: body.trim() }) + '\n')
process.exit(isError ? 1 : 0)

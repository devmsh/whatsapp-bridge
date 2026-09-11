// Claude Agent SDK sidecar: answer ONE extraction question about ONE chunk.
//
// Deliberately not an agent. The old extractor was a 120-turn tool loop, and
// it is being replaced because that shape cannot be verified and cannot run on
// a local model. This asks one question and prints one JSON object, so the
// Claude and Ollama engines are directly comparable: switching between them
// measures the model, not two different architectures.
//
// Auth: uses the local Claude subscription (Max/Pro) — do NOT set
//   ANTHROPIC_API_KEY. Either be logged in via `claude`, or set
//   CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`).
//
// Input:  one JSON object on stdin: {system, user, schema, model}
// Output: one JSON object on stdout, matching the schema.

import { query } from '@anthropic-ai/claude-agent-sdk'

const raw = await new Promise((resolve, reject) => {
  let buf = ''
  process.stdin.setEncoding('utf8')
  process.stdin.on('data', (d) => (buf += d))
  process.stdin.on('end', () => resolve(buf))
  process.stdin.on('error', reject)
})

let input
try {
  input = JSON.parse(raw)
} catch (e) {
  process.stderr.write('bad input: ' + e.message + '\n')
  process.exit(2)
}

const system = `${input.system}

Reply with JSON only. No prose, no code fences, no explanation. The reply must
match this schema exactly:

${JSON.stringify(input.schema, null, 2)}`

const response = query({
  prompt: input.user,
  options: {
    systemPrompt: system,
    // No tools at all. Everything this needs is already in the prompt, and a
    // tool call here would be the agentic shape creeping back in.
    allowedTools: [],
    disallowedTools: [
      'Bash', 'Read', 'Write', 'Edit', 'NotebookEdit',
      'Glob', 'Grep', 'WebFetch', 'WebSearch', 'Task', 'Agent',
    ],
    permissionMode: 'bypassPermissions',
    maxTurns: 1,
    ...(input.model ? { model: input.model } : {}),
  },
})

let answer = ''
try {
  for await (const msg of response) {
    if (msg.type === 'assistant') {
      for (const block of msg.message?.content || []) {
        if (block.type === 'text' && block.text) answer += block.text
      }
    } else if (msg.type === 'result') {
      if (msg.result) answer = msg.result
    }
  }
} catch (e) {
  process.stderr.write('query failed: ' + (e?.message || e) + '\n')
  process.exit(1)
}

// Models sometimes wrap JSON in a fence despite being asked not to. Take the
// outermost object rather than failing over punctuation.
const start = answer.indexOf('{')
const end = answer.lastIndexOf('}')
if (start < 0 || end <= start) {
  process.stderr.write('no JSON in answer\n')
  process.exit(1)
}
process.stdout.write(answer.slice(start, end + 1) + '\n')

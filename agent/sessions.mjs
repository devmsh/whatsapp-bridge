// Read extraction history straight from the Claude Agent SDK's session store
// (no app DB). The bridge spawns this as a short-lived process.
//
//   node sessions.mjs list <tag> [match]   -> JSON array of runs (by tag or
//                                             firstPrompt-contains-match)
//   node sessions.mjs show <session_id>    -> JSON transcript (steps)
//
// Sessions live under the cwd they were created in, so run this with the same
// cwd as extract.mjs (the bridge sets that).

import { listSessions, getSessionMessages } from '@anthropic-ai/claude-agent-sdk'

const mode = process.argv[2]
const arg = process.argv[3]
const match = process.argv[4] || ''
const dir = process.cwd()

function out(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n')
}

if (mode === 'list') {
  const tag = arg || ''
  const sessions = await listSessions({ dir }).catch(() => [])
  const runs = sessions
    .filter((s) => s.tag === tag || (match && (s.firstPrompt || '').includes(match)))
    .sort((a, b) => (b.lastModified || 0) - (a.lastModified || 0))
    .map((s) => ({
      session_id: s.sessionId,
      title: s.customTitle || s.summary || 'Extraction',
      first_prompt: s.firstPrompt || '',
      last_modified: s.lastModified || 0,
      created_at: s.createdAt || 0,
    }))
  out({ runs })
  process.exit(0)
}

if (mode === 'show') {
  const sessionId = arg
  if (!sessionId) {
    out({ error: 'session id required' })
    process.exit(2)
  }
  const messages = await getSessionMessages(sessionId, { dir }).catch((e) => {
    out({ error: String(e?.message || e) })
    process.exit(1)
  })
  const idToName = {}
  const steps = []
  for (const m of messages) {
    const payload = m.message || {}
    const blocks = Array.isArray(payload.content) ? payload.content : []
    for (const b of blocks) {
      if (b.type === 'text' && b.text) {
        steps.push({ type: m.type === 'assistant' ? 'assistant_text' : 'text', text: b.text })
      } else if (b.type === 'tool_use') {
        idToName[b.id] = b.name
        steps.push({ type: 'tool_use', name: b.name, input: b.input })
      } else if (b.type === 'tool_result') {
        // tool_result.content can be a string or an array of {type:'text',text}
        let content = ''
        if (typeof b.content === 'string') content = b.content
        else if (Array.isArray(b.content))
          content = b.content.map((c) => (typeof c === 'string' ? c : c.text || '')).join('\n')
        steps.push({
          type: 'tool_result',
          name: idToName[b.tool_use_id] || '',
          is_error: !!b.is_error,
          content,
        })
      }
    }
  }
  out({ session_id: sessionId, steps })
  process.exit(0)
}

out({ error: 'usage: sessions.mjs list <chat_jid> | show <session_id>' })
process.exit(2)

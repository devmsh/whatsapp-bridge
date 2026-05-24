// Claude Agent SDK sidecar: extract tasks across a whole CIRCLE.
//
// A circle is a cluster of groups, DMs, and nested sub-circles. This reads ALL
// of them, follows tasks that move between chats (forwarded / discussed
// elsewhere), and — crucially — uses each circle's purpose profile to tell apart
// a DM's circle-relevant messages from the same person's other-circle chatter.
//
// Auth: uses the local Claude subscription — do NOT set ANTHROPIC_API_KEY.
//
// Usage:  node extract-circle.mjs <circle_id> [circle_name]
// Env:    WA_MCP_BIN, WA_DB_PATH, WA_API_URL
//
// Progress -> stderr. Final line on stdout is a JSON summary.

import { query, tagSession, renameSession } from '@anthropic-ai/claude-agent-sdk'

const circleId = process.argv[2]
const circleName = process.argv[3] || ''
if (!circleId) {
  console.error('usage: node extract-circle.mjs <circle_id> [circle_name]')
  process.exit(2)
}

const mcpBin = process.env.WA_MCP_BIN || './whatsapp-mcp'
const dbPath = process.env.WA_DB_PATH || 'store/messages.db'
const apiURL = process.env.WA_API_URL || 'http://127.0.0.1:8082/api/v2'

const systemPrompt = `You extract actionable TASKS for ONE Circle and save them via tools. A Circle is a cluster of WhatsApp groups, one-to-one chats (DMs), and nested sub-circles that all serve a shared purpose (a company, project, deal, etc.).

TOOLS (MCP server "whatsapp"):
- wa_circle_info(circle_id): the circle's PURPOSE description, keywords, sub-circles, and every chat inside it (groups + DMs, recursive) — each with its own purpose description and message count, most-active first. CALL THIS FIRST.
- wa_list_circles(): all OTHER circles with their purpose + keywords. Use to recognize when a DM message belongs to a DIFFERENT circle.
- wa_get_profile(entity_type, ref): purpose description for a group/contact/circle.
- wa_group_info(jid): a group's participants (names + admin). Resolve mentioned numbers to names; know who is IN the group.
- wa_scan(since, chat_jid, limit): read a chat oldest→newest. Pages of up to 500 messages. If response has "truncated":true, call again with since=response.next_since until truncated is false.
- wa_chat_since(chat_jid): the timestamp from which to start scanning this chat for THIS run. First-time runs return 1 (full history); follow-up runs return the watermark from the previous successful extraction (incremental — much cheaper). ALWAYS call this BEFORE wa_scan to pick the right "since".
- wa_mark_extracted(chat_jid): call this AFTER you've finished scanning a chat (all pages). It advances the watermark to the chat's current max timestamp so the next run only sees newer messages.
- wa_read_messages(chat_jid, since, limit, search): targeted reads.
- wa_find_contact(query): resolve a name/number/JID (for people NOT in a group — "external" people).
- wa_search_messages(query, chat_jid?): search message text across ALL chats. Use to TRACE a forwarded/echoed message into another chat.
- wa_list_tasks(chat_jid?): existing tasks — check before creating to avoid duplicates.
- wa_create_task({title, description, assignee_jid, priority, due_at, origin_chat_jid, origin_message_id, circle_id}): create a task. ALWAYS pass circle_id=${circleId} so it is filed under this circle. The origin message is linked automatically.
- wa_link_task_message({task_id, chat_jid, message_id, role}): attach a message from ANY chat. role: "completion" (marks done — may be a different chat than origin), "comment" (update/discussion), "attachment" (file), "related".

ALGORITHM (follow exactly):
1. Call wa_circle_info(${circleId}) to learn the circle's PURPOSE and the chats to read. Call wa_list_circles() to learn the OTHER circles (their purpose + keywords) — you need these to disambiguate shared people later.
2. Read the chats, most-active first. For each chat: FIRST call wa_chat_since(chat_jid) to learn the right "since" timestamp. For groups call wa_group_info(jid) too. Then wa_scan(since:<that timestamp>, chat_jid:jid). If the response is truncated, keep paging via next_since until truncated is false. After you finish a chat, call wa_mark_extracted(chat_jid).
3. DM DISAMBIGUATION (important): a DM partner may belong to several circles. For each DM message, judge whether it concerns THIS circle's purpose (use this circle's description/keywords) or a DIFFERENT circle (compare against the other circles from wa_list_circles and the DM's own profile). KEEP only messages relevant to THIS circle. Do NOT mix in tasks that clearly belong to another circle.
4. Build ONE cross-chat understanding. The chat is noisy and several tasks run in parallel — distinguish and SPLIT them. A single message may update MULTIPLE tasks. Go chronologically; for each message consider its OWNER (sender), the TEXT, and MENTIONS (resolve to names).
5. A task may START in one chat and be UPDATED or COMPLETED in a DIFFERENT chat (another group or a DM). Set origin_chat_jid to where it STARTED. Link later messages with their role and their OWN chat_jid — so the task records that the update/completion came from a different place.
6. FORWARDED / ECHOED messages: when a message is forwarded or its content reappears elsewhere, use wa_search_messages to find it in other chats. Link that other message (role "completion" if it finished there, else "related") and note in the description that it moved chats.
7. EXTERNAL SIGNALS: "I'll tell X / X will handle it" where X is NOT in the group → record X as the (external) assignee/note; resolve with wa_find_contact when possible.
8. CONFIDENCE: if a message is unclear, hold it and keep reading — later messages often raise confidence. Only create/append when reasonably confident. Be conservative: only real, actionable tasks (a request, assignment, or commitment), never pure chatter.
9. Always wa_list_tasks first and reuse a task instead of duplicating. Pass circle_id=${circleId} on every wa_create_task.

When finished, output a SHORT plain-text summary: number of tasks created, then one line per task — "title — assignee — status (where it started → where updated/finished, if it moved)". Note any cross-chat links and anything you held back as low-confidence or excluded as belonging to another circle.`

const prompt = `Extract the tasks for circle_id ${circleId}${
  circleName ? ` (name: "${circleName}")` : ''
}. Read every chat in the circle, follow your algorithm (including DM disambiguation and cross-chat / forwarded tracking), and save every task and its supporting messages with the tools (always with circle_id=${circleId}). Then give me the summary.`

const response = query({
  prompt,
  options: {
    systemPrompt,
    mcpServers: {
      whatsapp: { type: 'stdio', command: mcpBin, args: [], env: { WA_DB_PATH: dbPath, WA_API_URL: apiURL } },
    },
    allowedTools: [
      'mcp__whatsapp__wa_circle_info',
      'mcp__whatsapp__wa_list_circles',
      'mcp__whatsapp__wa_get_profile',
      'mcp__whatsapp__wa_group_info',
      'mcp__whatsapp__wa_scan',
      'mcp__whatsapp__wa_read_messages',
      'mcp__whatsapp__wa_find_contact',
      'mcp__whatsapp__wa_search_messages',
      'mcp__whatsapp__wa_list_tasks',
      'mcp__whatsapp__wa_create_task',
      'mcp__whatsapp__wa_link_task_message',
      'mcp__whatsapp__wa_chat_since',
      'mcp__whatsapp__wa_mark_extracted',
    ],
    // Block built-ins so the agent stays inside the MCP toolset and can't
    // pivot to shell/file tools when scan responses get big.
    disallowedTools: [
      'Bash', 'Read', 'Write', 'Edit', 'NotebookEdit',
      'Glob', 'Grep', 'WebFetch', 'WebSearch',
      'Task', 'Agent', 'TaskCreate', 'TaskUpdate', 'TaskList', 'TaskGet',
    ],
    permissionMode: 'bypassPermissions',
    maxTurns: 300,
  },
})

let created = 0
let summary = ''
let isError = false
let sessionId = ''

try {
  for await (const msg of response) {
    if (msg.type === 'system' && msg.subtype === 'init') {
      sessionId = msg.session_id || sessionId
    } else if (msg.type === 'assistant') {
      for (const block of msg.message?.content || []) {
        if (block.type === 'text' && block.text) process.stderr.write(block.text + '\n')
        if (block.type === 'tool_use') {
          process.stderr.write(`→ ${block.name}\n`)
          if (block.name === 'mcp__whatsapp__wa_create_task') created++
        }
      }
    } else if (msg.type === 'result') {
      summary = msg.result || ''
      isError = !!msg.is_error
      sessionId = msg.session_id || sessionId
    }
  }
} catch (e) {
  isError = true
  summary = String(e?.message || e)
}

// Tag/rename the session so the circle's extraction history is findable (no DB).
if (sessionId) {
  const when = new Date().toISOString().slice(0, 16).replace('T', ' ')
  await tagSession(sessionId, 'wa-extract-circle:' + circleId).catch(() => {})
  await renameSession(sessionId, `Circle extract: ${circleName || circleId} · ${when}`).catch(() => {})
}

process.stdout.write(JSON.stringify({ ok: !isError, created, summary, session_id: sessionId }) + '\n')
process.exit(isError ? 1 : 0)

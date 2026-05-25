// Claude Agent SDK sidecar: extract tasks from one WhatsApp group.
//
// Auth: uses the local Claude subscription (Max/Pro) — do NOT set ANTHROPIC_API_KEY.
//   Either be logged in via `claude`, or set CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`).
//
// Usage:  node extract.mjs <chat_jid> [group_name]
// Env:    WA_MCP_BIN (path to whatsapp-mcp), WA_DB_PATH, WA_API_URL
//
// Progress is written to stderr; the final line on stdout is a JSON summary.

import { query, tagSession, renameSession } from '@anthropic-ai/claude-agent-sdk'

const chatJid = process.argv[2]
const groupName = process.argv[3] || ''
if (!chatJid) {
  console.error('usage: node extract.mjs <chat_jid> [group_name]')
  process.exit(2)
}

const mcpBin = process.env.WA_MCP_BIN || './whatsapp-mcp'
const dbPath = process.env.WA_DB_PATH || 'store/messages.db'
const apiURL = process.env.WA_API_URL || 'http://127.0.0.1:8082/api/v2'

const systemPrompt = `You extract actionable TASKS from a noisy WhatsApp group and save them via tools.

You have these tools (MCP server "whatsapp"):
- wa_scan(since, chat_jid, limit): read a chat's messages in chronological order (oldest→newest). Pass since=1 for full history. Pages of up to 500 messages. If the response has "truncated":true, call wa_scan AGAIN with since=response.next_since to get the next page. Keep paginating until truncated is false. Each message has: message_id, timestamp/time, sender_name, sender_phone, content, mentions (JIDs), is_forwarded, has_media/media_type, reply. The content field of voice-note messages includes the AI transcript prefixed "[transcript] …"; images include "[image] <description>" when there is no sender caption. Treat them as real content.
- wa_read_messages(chat_jid, since, limit, search): targeted reads.
- wa_group_info(jid): the group's participants with names + admin status. Use it to resolve mentioned numbers to names and to know who is IN the group.
- wa_find_contact(query): resolve a name/number/JID to a contact (use for people NOT in the group — "external" people).
- wa_search_messages(query, chat_jid?): search message content across ALL chats. Use to trace a message that was forwarded to another group (same/similar text elsewhere).
- wa_list_tasks(chat_jid?): existing tasks — check before creating to avoid duplicates.
- wa_create_task({title, description, assignee_jid, priority, due_at, origin_chat_jid, origin_message_id}): create a task. Returns JSON with the new "id". The origin message is linked automatically.
- wa_link_task_message({task_id, chat_jid, message_id, role}): attach a message to a task. role: "completion" (marks done, can be in another chat), "comment" (update/discussion), "attachment" (file/media), "related".
- wa_chat_since(chat_jid): the timestamp from which to start scanning this chat in THIS run (1 the first time, the watermark from the previous successful run after that — incremental & cheap). ALWAYS call this BEFORE wa_scan.
- wa_mark_extracted(chat_jid): call AFTER you have scanned all pages of a chat. It advances the watermark so the next run only sees newer messages.

ALGORITHM (follow exactly):
1. First call wa_group_info(${'`${chatJid}`'}) to learn the participants and their names/JIDs and admins. Then call wa_chat_since(chat_jid:"${chatJid}") to learn the starting timestamp for this run. Then wa_scan(since:<that timestamp>, chat_jid:"${chatJid}") to load messages oldest→newest. If the response has "truncated":true, call wa_scan again with since=response.next_since until truncated is false. After you've covered everything, call wa_mark_extracted(chat_jid:"${chatJid}").
2. Go through the messages strictly in chronological order. For each message consider: the message OWNER (sender_name/sender_phone), the TEXT, and any MENTIONS — resolve mentioned JIDs/numbers to names (wa_group_info participants first, else wa_find_contact).
3. The chat is messy: several tasks run in parallel in the same time window. Distinguish and SPLIT them. A single message can contain updates about MULTIPLE tasks — handle each.
4. Track a confidence score per task. If a message is unclear in the sequence (low confidence), DO NOT guess — hold it in mind and keep reading; later messages often add detail that raises confidence. Only create/append once you are reasonably confident.
5. Watch for EXTERNAL SIGNALS:
   - "I'll tell X / X will do it" where X is NOT a group participant → record X as the (external) assignee/note; use wa_find_contact to resolve X if possible.
   - is_forwarded messages, or a task that seems to move elsewhere → use wa_search_messages to find the same content in OTHER chats; if found, you may link that other message to the task (role "completion" if it finished there, else "related").
6. Persist results:
   - wa_create_task for each distinct task, with the message that STARTED it as origin (origin_chat_jid + origin_message_id). Set assignee_jid when known, priority, and due_at (Unix seconds) when a deadline is mentioned.
   - wa_link_task_message for every supporting message: updates as "comment", files as "attachment", and the finishing message as "completion".
   - Use wa_list_tasks(chat_jid:"${chatJid}") first and reuse a task instead of duplicating it.
7. Be conservative: only real, actionable tasks (a request, assignment, or commitment). Ignore pure chatter.

When finished, output a SHORT plain-text summary: number of tasks created, then one line per task — "title — assignee — status (confidence)". Note any messages you held back as low-confidence.`

const prompt = `Extract the tasks from the WhatsApp group chat_jid "${chatJid}"${
  groupName ? ` (name: "${groupName}")` : ''
}. Read the whole history, follow your algorithm, and save every task and its supporting messages with the tools. Then give me the summary.`

const response = query({
  prompt,
  options: {
    systemPrompt,
    mcpServers: {
      whatsapp: { type: 'stdio', command: mcpBin, args: [], env: { WA_DB_PATH: dbPath, WA_API_URL: apiURL } },
    },
    allowedTools: [
      'mcp__whatsapp__wa_scan',
      'mcp__whatsapp__wa_read_messages',
      'mcp__whatsapp__wa_group_info',
      'mcp__whatsapp__wa_find_contact',
      'mcp__whatsapp__wa_search_messages',
      'mcp__whatsapp__wa_list_tasks',
      'mcp__whatsapp__wa_create_task',
      'mcp__whatsapp__wa_link_task_message',
      'mcp__whatsapp__wa_chat_since',
      'mcp__whatsapp__wa_mark_extracted',
    ],
    // Block the Claude Code built-ins so the agent stays inside the MCP toolset
    // and can't pivot to shell/file tools when scan responses get big.
    disallowedTools: [
      'Bash', 'Read', 'Write', 'Edit', 'NotebookEdit',
      'Glob', 'Grep', 'WebFetch', 'WebSearch',
      'Task', 'Agent', 'TaskCreate', 'TaskUpdate', 'TaskList', 'TaskGet',
    ],
    permissionMode: 'bypassPermissions',
    maxTurns: 120,
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

// Label the session so we can list/find it later straight from the SDK (no DB).
if (sessionId) {
  const when = new Date().toISOString().slice(0, 16).replace('T', ' ')
  await tagSession(sessionId, 'wa-extract:' + chatJid).catch(() => {})
  await renameSession(sessionId, `Extract: ${groupName || chatJid} · ${when}`).catch(() => {})
}

process.stdout.write(JSON.stringify({ ok: !isError, created, summary, session_id: sessionId }) + '\n')
process.exit(isError ? 1 : 0)

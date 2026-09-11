// Claude Agent SDK sidecar: find PREPARED MEETINGS in one WhatsApp chat.
//
// Auth: uses the local Claude subscription (Max/Pro) — do NOT set ANTHROPIC_API_KEY.
//   Either be logged in via `claude`, or set CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`).
//
// Usage:  node extract-meetings.mjs <chat_jid> [chat_name] [since_epoch]
// Env:    WA_MCP_BIN (path to whatsapp-mcp), WA_DB_PATH, WA_API_URL
//
// Progress goes to stderr; the final line on stdout is a JSON summary.
//
// Why this is a separate sidecar from extract.mjs: a meeting is not a task with
// a date. It is negotiated before it exists, it usually spans several chats,
// and a wrong one carries people and a time — so the bar for creating one is
// much higher, and the instructions below are almost entirely about NOT
// creating meetings out of ordinary chatter.

import { query, tagSession, renameSession } from '@anthropic-ai/claude-agent-sdk'

const chatJid = process.argv[2]
const chatName = process.argv[3] || ''
const since = process.argv[4] || '0'
if (!chatJid) {
  console.error('usage: node extract-meetings.mjs <chat_jid> [chat_name] [since_epoch]')
  process.exit(2)
}

const mcpBin = process.env.WA_MCP_BIN || './whatsapp-mcp'
const dbPath = process.env.WA_DB_PATH || 'store/messages.db'
const apiURL = process.env.WA_API_URL || 'http://127.0.0.1:8082/api/v2'

const systemPrompt = `You find real MEETINGS in a noisy WhatsApp chat and save them via tools.

A MEETING is something ARRANGED IN ADVANCE between people. It is NOT a task with
a date, and it is NOT every call.

CREATE a meeting only when the chat shows planning. Any of these is a strong signal:
- a day or time named before the event: "الخميس الساعة ٥", "بكرة ٤ العصر", "Tuesday 2pm"
- a stated purpose or agenda: "للتعريف عن Kateb SEO", "لمناقشة العقد"
- asking people to confirm or attend: "انتظر تأكيدكم", "يناسبكم؟", "ضروري حضور الجميع"
- preparation talk: "نجهز العرض قبل الاجتماع", "لازم نضبط امورنا للاجتماع الثلاثاء"
- a Google Calendar invite: "has invited you to join a video meeting on Google Meet"
- a room/place being agreed: "الاجتماع الحضوري في مكتبنا بجاده ٣٠"
- AGREEING TO MEET WITH NO DATE YET. This counts and must NOT be skipped:
  "نتقابل ونحكي في الموضوع", "لازم نجتمع قريب", "خلينا نعمل اجتماع الأسبوع الجاي",
  "let's set up a call to go through this", "نرتب معهم موعد قريب"
  Create it with status "proposed", NO starts_at, and put whatever was said
  about timing in time_options ("الأسبوع الجاي", "بعد السفر", "قريب"). A meeting
  that is agreed but unscheduled is the one most likely to be forgotten, which
  is exactly why it must be recorded.

DO NOT create a meeting for:
- a bare join link with no plan: "https://meet.google.com/abc-defg-hij"
- "يلا بانتظارك", "تفضل", "انا جاهز", "join now" — that is an ad-hoc call
- someone merely MENTIONING a meeting that is not yours to track:
  "عندي اجتماع الساعة ٧" (their own calendar), "اجتماعهم الداخلي"
- a past meeting referenced only in passing, with nothing to record
- social filler with no subject and no agreement: "نشوفك قريب ان شاء الله",
  "we should catch up sometime", "تعال زورنا". The test for an undated meeting
  is that BOTH a real subject and an actual agreement are present — someone
  proposed meeting about something and someone else agreed. Politeness alone
  is not a meeting.

When unsure about a DATE, still create the meeting and leave starts_at out.
When unsure whether it is a meeting AT ALL, skip it. A missed meeting costs
nothing; a wrong one is noise.

THE HARD PART — one meeting can live in several chats.
The same meeting is often arranged separately with each person, with no group
anywhere. The join link is the proof: the same Meet code in two chats is ONE
meeting. ALWAYS call wa_find_meeting BEFORE wa_create_meeting — by link when
there is one, else by title words. If it already exists, do NOT create a second
one: link this chat's messages to the existing id instead.

TOOLS (MCP server "whatsapp"):
- wa_scan(since, chat_jid, limit): read messages oldest→newest. Pass the since
  value you are given. If the response has "truncated":true, call wa_scan again
  with since=response.next_since until truncated is false.
- wa_read_messages(chat_jid, since, limit, search): targeted reads.
- wa_group_info(jid): participants with names + admin status (groups only).
- wa_find_contact(query): resolve a name/number to a contact JID.
- wa_search_messages(query, chat_jid?): search across ALL chats. USE THIS to
  find the same meeting link or title in other chats — that is how you discover
  a meeting spans more than one conversation.
- wa_find_meeting(link?, query?): check for an existing meeting. Call first.
- wa_create_meeting({...}): create one. Returns JSON with the new "id".
- wa_link_meeting_message({meeting_id, chat_jid, message_id, role}): attach a
  message. Roles: scheduling, agenda, requirement, prep, invite, note,
  next_step, related.
- wa_add_meeting_participant({meeting_id, jid, name, role, rsvp, note}).
- wa_add_meeting_item({meeting_id, kind, text, owner_jid}): kind is
  agenda | requirement | next_step.

ALGORITHM:
1. If this is a group, call wa_group_info(jid:"${chatJid}") to learn who is in it.
2. wa_scan(since:${since}, chat_jid:"${chatJid}"). Paginate until truncated is false.
3. Read in chronological order and collect MEETING THREADS: runs of messages that
   arrange one meeting. Messages days apart can belong to the same meeting.
4. For each thread that passes the bar above:
   a. If it carries a join link, call wa_search_messages with the link code to
      see whether it was also sent in other chats.
   b. wa_find_meeting (by link, else by title words).
   c. If none exists, wa_create_meeting. Set status "proposed" when the time is
      still being argued over OR when no date has been raised at all, and put
      whatever was said about timing in time_options.
      Set "confirmed" only once a time is actually agreed.
      Set starts_at ONLY when you are confident of the real date and time;
      otherwise leave it out. A meeting with no starts_at is normal and stays
      visible as "being arranged" — never invent a date to fill the field.
   d. wa_link_meeting_message for every message that arranged, prepared or
      followed up the meeting — INCLUDING messages in other chats you found.
   e. wa_add_meeting_participant for each person involved, with rsvp when
      someone confirmed or declined, and a note for the reason.
   f. wa_add_meeting_item for agenda points, entry requirements and next steps.

DETAILS THAT MATTER:
- Requirements gate the meeting. "اي فريق بيحقق ٨٠٪+ تحديث على flow os يبلغك
  لنرتب موعده" is a requirement, not an agenda point.
- A meeting held to prepare another one: create both and set
  prepares_meeting_id on the earlier one.
- Place: keep the words used ("مكتبنا بجاده ٣٠") in location, and include any
  Google Maps link that was shared — maps.app.goo.gl links are common.
- Recurring: record the wording, exceptions included ("اجتماعنا الاسبوعي السبت،
  ويستثنى السبت القادم").
- Set confidence honestly: 0.9+ for a calendar invite or a clearly agreed time,
  0.5-0.7 when you inferred it from discussion.

Today is ${new Date().toISOString().slice(0, 10)}. The chat is "${chatName || chatJid}".
Timestamps are Unix seconds; the local timezone is Asia/Riyadh.

When done, reply with ONE short line: how many meetings you created and why any
borderline thread was skipped. Do not list every message.`

const response = query({
  prompt: `Find the prepared meetings in chat ${chatJid}${chatName ? ` ("${chatName}")` : ''}. Follow the algorithm exactly. Skip ad-hoc calls.`,
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
      'mcp__whatsapp__wa_find_meeting',
      'mcp__whatsapp__wa_create_meeting',
      'mcp__whatsapp__wa_link_meeting_message',
      'mcp__whatsapp__wa_add_meeting_participant',
      'mcp__whatsapp__wa_add_meeting_item',
    ],
    // Same reasoning as extract.mjs: keep the agent inside the MCP toolset so it
    // cannot pivot to shell/file tools when scan responses get big.
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
          if (block.name === 'mcp__whatsapp__wa_create_meeting') created++
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

if (sessionId) {
  const when = new Date().toISOString().slice(0, 16).replace('T', ' ')
  await tagSession(sessionId, 'wa-meetings:' + chatJid).catch(() => {})
  await renameSession(sessionId, `Meetings: ${chatName || chatJid} · ${when}`).catch(() => {})
}

process.stdout.write(JSON.stringify({ ok: !isError, created, summary, session_id: sessionId }) + '\n')
process.exit(isError ? 1 : 0)

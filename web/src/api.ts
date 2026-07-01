// REST + SSE client for the WhatsApp Bridge API.
// In dev, Vite proxies /api to the Go bridge on :8082.
// In production, the SPA is served by the bridge itself (same origin).

export type AuthStateName =
  | 'connecting'
  | 'logged_out'
  | 'qr'
  | 'pairing'
  | 'connected'
  | 'error'

export interface DeviceInfo {
  jid: string
  lid?: string
  push_name?: string
  platform?: string
  business_name?: string
}

export interface AuthState {
  state: AuthStateName
  qr_code?: string
  error?: string
  device?: DeviceInfo
  updated_at: number
}

export interface SyncProgress {
  history_batches: number
  last_sync_type?: string
  last_batch_at: number
  offline_total: number
  offline_done: number
  initial_sync_done: boolean
  updated_at: number
}

export type SyncPhase = 'starting' | 'receiving' | 'idle' | 'offline'

export interface SyncStatus {
  connected: boolean
  receiving: boolean
  phase: SyncPhase
  progress: SyncProgress
  counts: { messages: number; chats: number; contacts: number }
}

export interface MediaPolicy {
  images: boolean
  video: boolean
  audio: boolean
  documents: boolean
  stickers: boolean
  max_size_mb: number
}

export interface HistorySettings {
  period: string
  options: string[]
  note?: string
}

// ChatEvent mirrors the bridge's db.EventLog row — one structured
// protocol-level event scoped to a chat. event_type today is one of:
//   "ephemeral_setting" — disappearing-messages timer changed; `data` is
//                         JSON like `{"timer": 604800}` (seconds).
// More event types (group_added, group_left, etc) are planned upstream.
export interface ChatEvent {
  id: number
  event_type: string
  jid: string
  actor_jid?: string
  data?: string
  timestamp: number
}

// RecentSticker is one row in the composer's sticker tray — a bridge
// path + MIME + when it was last seen. Path is store-relative (e.g.
// "store/stickers/abc.webp"); mediaURL() turns it into a real URL.
export interface RecentSticker {
  path: string
  mime: string
  timestamp: number
}

// Newsletter mirrors the bridge's db.Newsletter row — one WA "channel"
// the user follows. JID ends in @newsletter; VerificationState is
// "VERIFIED" on the green-check ones, "" otherwise.
export interface Newsletter {
  jid: string
  name: string
  description?: string
  subscriber_count: number
  verification_state?: string
  picture_id?: string
  picture_url?: string
  invite_code?: string
  role?: string
  muted?: string
  state?: string
  creation_time?: number
  updated_at: number
}

// LinkedDevice is one row in the WA Settings → Linked devices list — a
// JID with the bare booleans the UI needs to render a "this is the
// primary phone" / "this is the current session" badge.
export interface LinkedDevice {
  jid: string
  is_primary: boolean
  is_current: boolean
}
export interface LinkedDevicesResponse {
  current: string
  devices: LinkedDevice[]
}

// PrivacySettings mirrors whatsmeow's types.PrivacySettings — every field is
// one of a small string enum. Empty string = WA hasn't synced it yet (we
// surface as "Default" in the UI). See api.privacy() for the value set per
// field.
export interface PrivacySettings {
  GroupAdd: string
  LastSeen: string
  Status: string
  Profile: string
  ReadReceipts: string
  CallAdd: string
  Online: string
  Messages: string
}

export interface ChatPreview {
  chat_jid: string
  sender: string
  sender_name: string
  push_name: string
  content: string
  media_type: string
  media_caption: string
  is_from_me: boolean
  is_group: boolean
  is_deleted: boolean
  timestamp: number
}

export interface Chat {
  jid: string
  name: string
  chat_type: string
  last_message_at: number
  unread_count: number
  is_archived: boolean
  is_pinned: boolean
  is_muted: boolean
  is_hidden?: boolean // only true when returned in unlocked "private mode"
  last_message?: ChatPreview
  // Count of @-mentions of the current user inside this chat's unread
  // window. Used by the chat list to render the small '@' badge next to
  // the unread count, so a ping in a busy chat is obvious before opening
  // it. Absent (or 0) means no pending mentions.
  unread_mentions?: number
  // Mute end-time as a Unix second; 0 / absent = unmuted or muted "Always"
  // (no expiry). Bridge returns it on every chat row but most UIs only
  // need is_muted — kept here for the rare countdown surface.
  muted_until?: number
  // Disappearing-messages timer in seconds: 0 = off, 86400 = 24h,
  // 604800 = 7d, 7776000 = 90d. WA only accepts those four values; the
  // bridge round-trips them via PUT /chats/{jid}/disappearing.
  disappearing_timer?: number
}

export interface Reaction {
  message_id: string
  chat_jid: string
  sender: string
  sender_name: string
  emoji: string
  timestamp: number
}

export interface Message {
  id: string
  chat_jid: string
  sender: string
  sender_name: string
  push_name: string
  content: string
  timestamp: number
  is_from_me: boolean
  is_group: boolean
  message_type: string
  is_deleted?: boolean
  is_edit?: boolean
  is_forwarded?: boolean
  // Hop count for forwarded messages. WA's misinformation-warning surface
  // bumps the "Forwarded" chip to "Forwarded many times" once this hits 4
  // (or higher) — i.e. the message has been forwarded through 4+ hops since
  // it was originally composed. Absent / 0 for non-forwarded messages.
  forward_score?: number
  // True when WA flagged this message with the ephemeral bit — i.e. it was
  // sent inside a chat with disappearing-messages on. Cosmetic only on the
  // client (WA mobile shows a small clock badge under the bubble).
  is_ephemeral?: boolean
  // True when this is a view-once media message — the recipient can open
  // the photo / video once and then it self-destructs. The bridge captures
  // the media before that happens; we still render the "🔥 View once"
  // badge so the user knows the sender intended one-time viewing.
  is_view_once?: boolean
  media_type?: string
  media_path?: string
  media_mime?: string
  media_size?: number
  media_caption?: string
  media_filename?: string
  thumbnail_path?: string
  reply_to_id?: string
  reply_to_sender?: string
  reply_to_content?: string
  reactions?: Reaction[]
  chat_name?: string
  // When non-empty, this message is a WA poll. The full poll body
  // (question, options, votes) lives on /api/v2/polls/{id}; the bubble
  // fetches it lazily and renders the vote UI inline.
  poll_id?: string

  // AI-derived (only present when the media-understanding worker has run):
  transcript?: string         // voice-note transcript via whisper
  media_description?: string  // image caption via vision

  // Delivery state for our own outgoing messages (is_from_me=true). Absent on
  // received messages. Maps to WhatsApp's tick UX:
  //   sent      — accepted by server, no receipt yet (single grey ✓)
  //   delivered — receipt arrived from recipient device (double grey ✓✓)
  //   read      — recipient (or a group participant) opened the chat (blue ✓✓)
  //   played    — voice / video media playback receipt observed (blue ✓✓)
  status?: 'sent' | 'delivered' | 'read' | 'played'

  // True when the user has bookmarked this message via the Star action.
  // Returned by /messages (per-chat) and /starred (global).
  is_starred?: boolean
  // Unix seconds — only populated on the /starred global list.
  starred_at?: number

  // WA LocationMessage payload. Present whenever message_type is "location"
  // — both for our own /send-location echoes and for messages received via
  // the SSE stream. (lat, lng) are mandatory at the protocol level so they
  // come together; name / address are the "title / subtitle" lines WA
  // renders inside the location bubble.
  latitude?: number
  longitude?: number
  location_name?: string
  location_address?: string

  // WA ContactMessage payload (vCard share). The bridge stashes the
  // displayed name and the raw vCard body so the bubble can render a
  // "Contact card" chip with download / open / save actions.
  vcard_name?: string
  vcard_data?: string
}

// One row per (message, recipient, receipt-type) — what GET
// /api/v2/messages/{id}/receipts returns. receipt_type maps to WA's
// receipt state: "" (delivered), "read", "played" (audio/video).
export interface MessageReceipt {
  message_id: string
  chat_jid: string
  sender_jid: string
  receipt_type: '' | 'read' | 'played' | string
  timestamp: number
}

// One row per whatsmeow call event the bridge has observed (offer /
// accept / reject / terminate / timeout). A single real-world call usually
// shows up as several rows sharing call_id; the UI groups them by call_id
// to summarise "answered", "declined", or "missed".
export interface CallEvent {
  call_id: string
  from_jid: string
  timestamp: number
  call_creator?: string
  group_jid?: string
  event_type: string
  remote_platform?: string
  remote_version?: string
  data?: string
}

// Poll body — fetched on demand for any Message whose poll_id is set.
// `options` is a JSON-encoded array of strings (the bridge stores it as a
// blob, callers parse). `max_selections` = 1 → single-choice, > 1 →
// multi-choice up to that many.
export interface Poll {
  message_id: string
  chat_jid: string
  question: string
  options: string
  max_selections: number
  created_at: number
}

// One row per voter — selected_options is a JSON-encoded string[]. The
// bridge upserts so the latest vote per voter replaces older ones.
export interface PollVote {
  poll_message_id: string
  poll_chat_jid: string
  voter_jid: string
  selected_options: string
  timestamp: number
}

export interface PollDetail {
  poll: Poll
  votes: PollVote[] | null
}

export interface Contact {
  jid: string
  lid?: string
  phone?: string
  name: string
  push_name?: string
  business_name?: string
  /** WA-verified business name (green-check). Same field as the one on the
   *  ContactFull return — surfaced here too so the contact-list rendering can
   *  prefer it ahead of business_name / push_name when picking a label. */
  verified_name?: string
  is_business?: boolean
  // True when this contact's DM is in hidden_chats. The UI uses this to
  // trigger a per-chat fingerprint unlock when the user clicks a mention chip
  // for a hidden contact, without flipping the whole UI into private mode.
  is_hidden?: boolean
}

export interface Group {
  jid: string
  name: string
}

// GroupJoinRequest mirrors whatsmeow's types.GroupParticipantRequest — one
// user who tapped an invite link and is waiting for an admin to approve or
// reject. JID is the requester (phone-form), RequestedAt is the ISO time
// the request landed (so the UI can show "asked 2h ago").
export interface GroupJoinRequest {
  JID: string
  RequestedAt: string
}

// GroupFull is the richer object behind GET /api/v2/groups/{jid} — pulled
// from the bridge's local db.Group row. Exposes the admin-managed flags
// (is_announce, is_locked) so the Group info modal can render them.
export interface GroupFull {
  jid: string
  name: string
  topic?: string
  is_announce?: boolean
  is_locked?: boolean
  disappearing_timer?: number
  participants?: GroupParticipant[]
}

// GroupParticipant mirrors the bridge's GroupParticipant row, surfaced via
// GET /api/v2/groups/{jid}/participants. Used by the composer's @-picker.
export interface GroupParticipant {
  jid: string
  lid?: string
  phone?: string
  is_admin: boolean
  is_super_admin: boolean
  display_name?: string
}

// PresenceEntry mirrors the presence_cache row the bridge stores from
// whatsmeow's events.Presence / events.ChatPresence streams.
//   status = 'available' | 'unavailable' | 'composing' | 'paused'
//   last_seen — Unix seconds when the contact was last seen online.
//     0 when the contact has hidden last-seen via WhatsApp's privacy
//     settings (the UI should render nothing rather than 'last seen never').
//   updated_at — Unix seconds the bridge last refreshed this entry.
//     Used to age out stale 'composing' / 'available' (presence beacons
//     stop arriving when the contact closes WhatsApp; without freshness
//     we'd keep showing 'online' forever).
export interface PresenceEntry {
  jid: string
  status: 'available' | 'unavailable' | 'composing' | 'paused' | string
  last_seen?: number
  updated_at: number
}

export interface ChatStat {
  chat_jid: string
  count: number
  last_message_at: number
}

export type MemberType = 'group' | 'contact' | 'circle'

export interface Circle {
  id: number
  name: string
  color: string
  notes?: string
  created_at: number
  updated_at: number
  member_count: number
  keywords?: string[]
  child_circles?: number[]
  parent_ids?: number[] // direct parent-circle ids; empty for top-level
}

export interface MemberSuggestion {
  type: 'group' | 'contact'
  ref: string
  label: string
  keyword: string
}

export interface CircleSuggestions {
  context: string
  suggestions: MemberSuggestion[]
}

export interface Tag {
  id: number
  name: string
  color: string
  created_at: number
}

export type TaskStatus = 'open' | 'in_progress' | 'done' | 'cancelled'
export type TaskReviewStatus = 'pending_review' | 'accepted' | 'rejected'

export interface Task {
  id: number
  title: string
  description: string
  status: TaskStatus
  priority: string
  assignee_jid: string
  creator_jid: string
  due_at: number
  completed_at: number
  origin_chat_jid: string
  origin_message_id: string
  review_status: TaskReviewStatus
  parent_id?: number | null
  created_at: number
  updated_at: number
  message_count: number
  circle_ids?: number[]
}

// Result of POST /api/v2/tasks/cluster — how the cluster pass changed the tree.
export interface ClusterResult {
  new_parents: number
  reused_parents: number
  children_linked: number
  skipped: number
  rationales?: string[]
}

export interface TaskMessageLink {
  task_id: number
  chat_jid: string
  message_id: string
  role: string
  added_at: number
  sender?: string
  sender_name?: string
  push_name?: string
  content?: string
  timestamp?: number
  is_from_me?: boolean
  is_group?: boolean
  media_type?: string
  media_path?: string
}

export interface TaskDetail {
  task: Task
  messages: TaskMessageLink[]
}

// One past extraction run, read from the Agent SDK session store (no DB).
export interface ExtractionRun {
  session_id: string
  title: string
  first_prompt: string
  last_modified: number
  created_at: number
}

// A live extraction-run event (parsed from the sidecar's stderr).
export interface ExtractionRunEvent {
  ts: number
  seq: number
  kind: 'tool' | 'text' | 'info' | 'result' | 'error'
  name?: string
  text?: string
}

export type ExtractionRunStatus = 'starting' | 'running' | 'done' | 'failed' | 'cancelled'

// Live run state from GET /api/v2/extractions/runs/{id}.
export interface ExtractionRunState {
  id: string
  kind: 'chat' | 'circle'
  subject: string
  label: string
  status: ExtractionRunStatus
  started_at: number
  ended_at?: number
  session_id?: string
  created?: number
  summary?: string
  error?: string
  events?: ExtractionRunEvent[]
}

// One step in a run's transcript: agent reasoning, a tool call, or its result.
export interface ExtractionStep {
  type: 'assistant_text' | 'text' | 'tool_use' | 'tool_result'
  text?: string
  name?: string
  input?: unknown
  is_error?: boolean
  content?: string
}

export type ProfileEntityType = 'circle' | 'group' | 'contact'

// An AI-written (and user-editable) purpose description for an entity.
export interface EntityProfile {
  entity_type: ProfileEntityType
  entity_ref: string
  description: string
  source: 'auto' | 'manual'
  msg_count_at_gen: number
  status: 'pending' | 'ok' | 'empty' | 'error'
  error?: string
  generated_at: number
  updated_at: number
}

export interface ProfileStats {
  total: number
  ok: number
  empty: number
  error: number
  pending: number
  manual: number
  stale: number
  queue_size: number
}

export interface ProfilesStatus {
  stats: ProfileStats
  active: string[]
  enabled: boolean
}

// Daily briefing — AI-written digest of tasks + recent signal + awaiting-reply.
export interface BriefingTask {
  id: number
  title: string
  priority: string
  status: string
  assignee?: string
  assignee_jid?: string
  due_at?: number
  circle_name?: string
}

export interface BriefingChat {
  jid: string
  name: string
  last_active_at: number
  new_messages: number
  narrative?: string
}

export interface BriefingAwaiting {
  jid: string
  name: string
  last_message_at: number
  last_from_name: string
  preview?: string
}

export interface BriefingPayload {
  for_date: string
  generated_at: number
  summary: string
  today: BriefingTask[]
  overdue: BriefingTask[]
  signal_chats: BriefingChat[]
  awaiting_reply: BriefingAwaiting[]
  stats_tasks_open: number
}

// Stored briefing row (the "data" field is BriefingPayload JSON).
export interface BriefingRow {
  id: number
  for_date: string
  data: string
  generated_at: number
}

export interface DashRecent {
  timestamp: number
  is_from_me: boolean
  sender_jid?: string
  from: string
  content: string
}

export interface DashContact {
  jid: string
  name: string
  phone?: string
  business_name?: string
  is_business?: boolean
  /** WA-verified business name. Non-empty only on accounts that have
   *  passed WhatsApp's official business verification (the green check
   *  mark). Drives the "✓ Verified" hero badge in Contact info. */
  verified_name?: string
  profile: EntityProfile | null
  tags: Tag[]
  circles: Circle[]
  tasks_open: Task[]
  tasks_done_count: number
  last_active: number
  message_count: number
  recent: DashRecent[]
}

export interface DashContributor {
  jid: string
  name: string
  messages: number
  is_admin?: boolean
}

// Universal search hit (any kind).
export interface SearchHit {
  kind: 'contact' | 'group' | 'circle' | 'task' | 'message'
  id: string
  title: string
  subtitle?: string
  snippet?: string
  chat_jid?: string
  ts?: number
}

// Voice + image understanding status.
export interface MediaUnderstandingStats {
  audio_total: number
  audio_transcribed: number
  audio_pending: number
  audio_error: number
  image_total: number
  image_described: number
  image_pending: number
  image_error: number
}

// Hidden / locked chats.
export interface HiddenStatus {
  pin_set: boolean
  webauthn_registered: boolean
  unlocked: boolean
  hidden_count: number
}

export interface HideChatPreview {
  jid: string
  is_group: boolean
  tasks_originated_here: number
  tasks_linked: number
  task_message_links: number
  profile_exists: boolean
  media_understanding_rows: number
  extraction_watermark_set: boolean
  circle_membership_count: number
}

export interface HideChatResult extends HideChatPreview {
  tasks_deleted: number
  task_links_deleted: number
  profile_deleted: boolean
  media_rows_deleted: number
  briefings_deleted: number
  circle_edges_deleted: number
  watermark_deleted: boolean
}

export interface MediaUnderstandingStatus {
  // True when the feature is parked behind the rate-limit guard (server-side
  // kill switch). When set, the per-kind toggles are forced OFF and disabled.
  disabled?: boolean
  audio_enabled: boolean
  image_enabled: boolean
  whisper_detected: boolean
  whisper_binary?: string
  stats: MediaUnderstandingStats
}

// Auto-extract scheduler status.
export interface AutoExtractStatus {
  enabled: boolean
  interval_hours: number
  running: boolean
  last_run_id?: string
  last_ticked_at?: number
}

export interface DashGroup {
  jid: string
  name: string
  topic?: string
  participant_count: number
  profile: EntityProfile | null
  circles: Circle[]
  tasks_open: Task[]
  tasks_done_count: number
  last_active: number
  message_count: number
  top_contributors: DashContributor[]
  recent: DashRecent[]
}

export interface CircleContact {
  jid: string
  group_count: number
  is_admin: boolean
  tags: Tag[]
}

export interface CircleMember {
  circle_id: number
  member_type: MemberType
  member_ref: string
  added_at: number
}

export interface CircleDetail {
  circle: Circle
  members: CircleMember[]
}

export interface RecMember {
  type: 'group' | 'contact'
  ref: string
  label: string
}

export interface Recommendation {
  id: string
  type: 'new_circle' | 'add_to_circle'
  title: string
  reason: string
  score: number
  name?: string
  color?: string
  circle_id?: number
  members: RecMember[]
}

export interface RecsResponse {
  active: Recommendation[]
  hidden: Recommendation[]
}

// WorkingHoursConfig is the working-hours auto-mute feature configuration.
// start/end are "HH:MM" 24-hour strings. working_days is a list of weekday
// numbers (0=Sunday … 6=Saturday). chat_jids is the explicit opt-in list of
// chats to auto-mute when outside working hours. feature_muted is read-only —
// it lists JIDs the feature has muted itself (do NOT send it on PUT).
export interface WorkingHoursConfig {
  enabled: boolean
  start: string
  end: string
  working_days: number[]
  chat_jids: string[]
  feature_muted: string[]
}

async function postJSON(path: string): Promise<void> {
  const res = await fetch(path, { method: 'POST' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `${res.status} ${res.statusText}`)
  }
}

async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || `${res.status}`)
  return data as T
}

async function postBody<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { error?: string }).error || `${res.status}`)
  return data as T
}

async function del(path: string, body?: unknown): Promise<void> {
  const res = await fetch(path, {
    method: 'DELETE',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const b = await res.json().catch(() => ({}))
    throw new Error((b as { error?: string }).error || `${res.status}`)
  }
}

export const api = {
  authStatus: async (): Promise<AuthState> => {
    const res = await fetch('/api/v2/auth/status')
    return res.json()
  },
  login: () => postJSON('/api/v2/auth/login'),
  logout: () => postJSON('/api/v2/auth/logout'),
  syncProgress: async (): Promise<SyncStatus> => {
    const res = await fetch('/api/v2/sync/progress')
    return res.json()
  },
  mediaSettings: async (): Promise<MediaPolicy> => {
    const res = await fetch('/api/v2/settings/media')
    return res.json()
  },
  setMediaSettings: (p: MediaPolicy) => putJSON<MediaPolicy>('/api/v2/settings/media', p),
  workingHours: async (): Promise<WorkingHoursConfig> => {
    const res = await fetch('/api/v2/working-hours')
    return res.json()
  },
  setWorkingHours: (cfg: WorkingHoursConfig) =>
    putJSON<WorkingHoursConfig>('/api/v2/working-hours', {
      enabled: cfg.enabled,
      start: cfg.start,
      end: cfg.end,
      working_days: cfg.working_days,
      chat_jids: cfg.chat_jids,
    }),
  historySettings: async (): Promise<HistorySettings> => {
    const res = await fetch('/api/v2/settings/history')
    return res.json()
  },
  setHistory: (period: string) =>
    putJSON<{ period: string; applied_now: boolean }>('/api/v2/settings/history', { period }),
  chats: async (): Promise<Chat[]> => {
    const res = await fetch('/api/v2/chats')
    return res.json()
  },
  chat: async (jid: string): Promise<Chat> => {
    const res = await fetch('/api/v2/chats/' + encodeURIComponent(jid))
    if (!res.ok) throw new Error('chat ' + res.status)
    return res.json()
  },
  messages: async (jid: string, limit = 100): Promise<Message[]> => {
    const res = await fetch(
      `/api/v2/messages?chat_jid=${encodeURIComponent(jid)}&limit=${limit}`,
    )
    return res.json()
  },
  send: (
    jid: string,
    message: string,
    opts?: {
      mediaPath?: string
      mentionedJIDs?: string[]
      viewOnce?: boolean
      /** When true, a .webp attachment is sent as a WA sticker (no caption,
       *  no compression) instead of a plain image. Bridge's /send already
       *  handles the wire-shape switch. */
      sticker?: boolean
    },
  ) =>
    postBody<{ success: boolean; message_id: string; timestamp: number }>('/api/v2/send', {
      jid,
      message,
      media_path: opts?.mediaPath,
      mentioned_jids: opts?.mentionedJIDs,
      view_once: opts?.viewOnce,
      sticker: opts?.sticker,
    }),
  // groupParticipants returns the full participant list of a group — used by
  // the composer's @-picker to suggest who you might be tagging.
  groupParticipants: async (jid: string): Promise<GroupParticipant[]> => {
    const res = await fetch(`/api/v2/groups/${encodeURIComponent(jid)}/participants`)
    if (!res.ok) return []
    return res.json()
  },
  // groupInviteLink returns the chat.whatsapp.com URL anyone with this group's
  // invite code can use to join. When `reset` is true the bridge asks
  // whatsmeow to revoke the old code and mint a new one — the previous link
  // immediately stops working, matching WA's "Reset link" action.
  // Admin-only on most groups; non-admins get a 500 from the bridge here.
  groupInviteLink: async (jid: string, reset = false): Promise<string> => {
    const q = reset ? '?reset=true' : ''
    // Bridge route is `/invite-link`, not `/invite` — cycle-59 typo fix.
    const res = await fetch(`/api/v2/groups/${encodeURIComponent(jid)}/invite-link${q}`)
    if (!res.ok) throw new Error('Failed to load invite link')
    const body = (await res.json()) as { link?: string }
    return body.link || ''
  },
  // groupRequests returns the pending join-requests for a group — admins
  // see this list in WA mobile under "Pending requests". Each entry is one
  // user who tapped a stale invite link or requested join when the group
  // doesn't auto-add. Empty array (not an error) when there's nothing
  // pending. Non-admin callers are rejected by whatsmeow with a 500.
  groupRequests: async (jid: string): Promise<GroupJoinRequest[]> => {
    const res = await fetch(`/api/v2/groups/${encodeURIComponent(jid)}/requests`)
    if (!res.ok) return []
    return res.json()
  },
  // groupRequestsUpdate approves or rejects pending join requests in batch.
  // Mirror of groupParticipantsUpdate's add/remove but routes through the
  // request-only endpoint so whatsmeow can decline cleanly.
  groupRequestsUpdate: (
    jid: string,
    jids: string[],
    action: 'approve' | 'reject',
  ) =>
    postBody<{ success: boolean }>(
      `/api/v2/groups/${encodeURIComponent(jid)}/requests`,
      { jids, action },
    ),
  // groupRename sets the visible group title. Admin-only; non-admins or
  // groups with is_locked=true (admins-only-can-edit-info) get a 500 from
  // whatsmeow that we surface up to the caller. The change propagates to
  // every member's WA client immediately.
  groupRename: (jid: string, name: string) =>
    fetch(`/api/v2/groups/${encodeURIComponent(jid)}/name`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to rename group')
      return r.json() as Promise<{ success: boolean }>
    }),
  // groupSetDescription sets the group's "Description" block (called "Topic"
  // upstream). Same admin/locked gating as rename. Pass '' to clear.
  groupSetDescription: (jid: string, description: string) =>
    fetch(`/api/v2/groups/${encodeURIComponent(jid)}/description`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ description }),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to update description')
      return r.json() as Promise<{ success: boolean }>
    }),
  // groupParticipantsUpdate adds, removes, promotes, or demotes participants
  // in one group. WhatsApp's underlying call is batched, so the bridge accepts
  // a list of JIDs and one action — pass [jid] for the per-row WA gestures
  // (promote / demote / remove from the group info member row).
  //
  // Non-admin callers are rejected upstream by whatsmeow; we surface the
  // error so the caller knows nothing changed. On success the returned set
  // shape (`{success: true}`) means every JID applied — partial successes
  // would come back as bridge errors via 5xx.
  groupParticipantsUpdate: (
    jid: string,
    jids: string[],
    action: 'add' | 'remove' | 'promote' | 'demote',
  ) =>
    postBody<{ success: boolean }>(
      `/api/v2/groups/${encodeURIComponent(jid)}/participants`,
      { jids, action },
    ),
  // groupGet returns the full group row + participants from the bridge —
  // /api/v2/groups/{jid}. Used by the admin settings section to read the
  // current Announce / Locked / Disappearing state so the pills show what's
  // actually applied instead of "unknown". Includes the participants list
  // (same shape groupParticipants() hands back).
  groupGet: async (jid: string): Promise<GroupFull> => {
    const res = await fetch(`/api/v2/groups/${encodeURIComponent(jid)}`)
    if (!res.ok) throw new Error('Failed to load group')
    return res.json()
  },
  // groupSettings flips the two binary group toggles WA exposes:
  //   announce: true  → only admins can send messages ("announcement" group)
  //   locked:   true  → only admins can change group name / photo / description
  // Bridge accepts either field individually so we can change one without
  // touching the other. Non-admins are rejected upstream by whatsmeow.
  groupSettings: (jid: string, settings: { announce?: boolean; locked?: boolean }) =>
    fetch(`/api/v2/groups/${encodeURIComponent(jid)}/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(settings),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to save group settings')
      return r.json() as Promise<{ success: boolean }>
    }),
  // upload posts a single file as multipart/form-data, the bridge writes it
  // under <MediaDir>/uploads/<yyyymm>/, and returns an absolute path that
  // /send and /reply can consume in media_path.
  upload: async (file: File): Promise<{ path: string; size: number; mime: string; filename: string }> => {
    const fd = new FormData()
    fd.append('file', file, file.name)
    const res = await fetch('/api/v2/uploads', { method: 'POST', body: fd })
    if (!res.ok) {
      const t = await res.text().catch(() => '')
      throw new Error(`upload failed: ${res.status} ${t || res.statusText}`)
    }
    return res.json()
  },
  // reply quotes the message at `quotedID` and posts the new text into the same
  // chat. The bridge resolves the quoted body server-side, so callers only need
  // to send the ID — same shape as official WA's "reply" composer. Pass
  // opts.mediaPath (returned by api.upload) to reply with a photo/file.
  reply: (
    jid: string,
    quotedID: string,
    message: string,
    opts?: { mediaPath?: string; mentionedJIDs?: string[] },
  ) =>
    postBody<{ success: boolean; message_id: string; timestamp: number }>('/api/v2/reply', {
      chat_jid: jid,
      message_id: quotedID,
      message,
      media_path: opts?.mediaPath,
      mentioned_jids: opts?.mentionedJIDs,
    }),
  // Presence: subscribe asks the bridge to push presence updates for this
  // contact (whatsmeow only delivers them after the first subscribe). get
  // reads the cached entry; we poll it while the chat is open so the header
  // can show 'online' / 'typing…' / 'last seen X', the way official WA does.
  presenceSubscribe: (jid: string) =>
    postBody<{ success: boolean }>('/api/v2/presence/subscribe', { jid }),
  presenceGet: async (jid: string): Promise<PresenceEntry | null> => {
    const res = await fetch('/api/v2/presence/' + encodeURIComponent(jid))
    if (!res.ok) return null
    return res.json()
  },
  // chatCalls returns the call events scoped to this chat — used to render
  // WA-style inline "📞 Voice call" pills in the timeline. The bridge logs
  // every event_type row (offer / accept / terminate / etc); the client
  // coalesces them into one pill per call_id before rendering.
  chatCalls: async (jid: string, limit = 200): Promise<CallEvent[]> => {
    const res = await fetch(
      `/api/v2/chats/${encodeURIComponent(jid)}/calls?limit=${limit}`,
    )
    if (!res.ok) return []
    return res.json()
  },
  // chatEvents returns the protocol-level events scoped to this chat — the
  // small grey "Disappearing messages set to 7 days" / "X joined" pills WA
  // renders inline. Today only ephemeral_setting (disappearing-timer
  // changes) gets logged; the bridge will grow more event types over time.
  // Sorted newest-first; the timeline merges by timestamp on the client.
  chatEvents: async (jid: string, limit = 200): Promise<ChatEvent[]> => {
    const res = await fetch(
      `/api/v2/chats/${encodeURIComponent(jid)}/events?limit=${limit}`,
    )
    if (!res.ok) return []
    return res.json()
  },
  // chatTyping returns the sender JIDs currently typing in this chat (group
  // header polls it for "X is typing…"). The bridge keeps an ephemeral
  // in-memory cache fed by events.ChatPresence — entries auto-expire after
  // ~10s without a refresh.
  chatTyping: async (jid: string): Promise<string[]> => {
    const res = await fetch('/api/v2/chats/' + encodeURIComponent(jid) + '/typing')
    if (!res.ok) return []
    return res.json()
  },
  // typingSnapshot returns every chat with at least one fresh 'composing'
  // beacon — groups + DMs in one response, keyed by chat JID. Used by the
  // chat list to render "typing…" previews without per-row polling.
  typingSnapshot: async (): Promise<Record<string, string[]>> => {
    const res = await fetch('/api/v2/typing')
    if (!res.ok) return {}
    const body = (await res.json()) as { chats?: Record<string, string[]> }
    return body.chats || {}
  },
  // calls returns the most recent call events the bridge has seen, newest
  // first. limit caps the row count (default 100 server-side). One real
  // call shows up as several rows (offer/accept/terminate/...); the UI
  // groups by call_id to render one row per call.
  calls: async (limit = 100): Promise<CallEvent[]> => {
    const res = await fetch('/api/v2/calls?limit=' + limit)
    if (!res.ok) return []
    return res.json()
  },
  // createPoll posts a fresh poll into a chat. Backend uses whatsmeow's
  // BuildPollCreation (which adds the MessageSecret) and persists the poll
  // body locally so the bubble can render it immediately. Returns the new
  // poll's message_id so the caller can echo a bubble.
  createPoll: (
    jid: string,
    question: string,
    options: string[],
    maxSelections = 1,
  ) =>
    postBody<{ success: boolean; message_id: string }>('/api/v2/polls', {
      chat_jid: jid,
      question,
      options,
      max_selections: maxSelections,
    }),
  // getPoll fetches the poll body + every recorded vote for a poll message.
  // Bridge endpoint: GET /api/v2/polls/{id}?chat_jid=... → {poll, votes}.
  // Useful for rendering the poll bubble with live tallies.
  getPoll: async (jid: string, messageID: string): Promise<PollDetail | null> => {
    const res = await fetch(
      `/api/v2/polls/${encodeURIComponent(messageID)}?chat_jid=${encodeURIComponent(jid)}`,
    )
    if (!res.ok) return null
    return res.json()
  },
  // votePoll casts the current user's selection. The bridge re-encrypts the
  // vote against the poll's metadata + ships it via whatsmeow.BuildPollVote.
  // `options` is the raw option strings the user picked.
  votePoll: (jid: string, messageID: string, options: string[]) =>
    postBody<{ success: boolean }>(`/api/v2/polls/${encodeURIComponent(messageID)}/vote`, {
      chat_jid: jid,
      options,
    }),
  // messageReceipts returns one row per recipient × receipt-type for an
  // outgoing message. Empty receipt_type = delivered; "read" + "played"
  // are the upgraded states. Drives the Message Info screen — caller
  // typically buckets into Read vs Delivered for display.
  messageReceipts: async (jid: string, messageID: string): Promise<MessageReceipt[]> => {
    const res = await fetch(
      `/api/v2/messages/${encodeURIComponent(messageID)}/receipts?chat_jid=${encodeURIComponent(jid)}`,
    )
    if (!res.ok) return []
    return res.json()
  },
  // revoke ("delete for everyone") retracts a sent message. Bridge endpoint
  // POST /messages/{id}/revoke uses whatsmeow.RevokeMessage; the bubble's
  // is_deleted flag flips immediately on the local DB row, and the UI
  // re-renders as "🚫 This message was deleted". WA enforces a server-side
  // window for this; past that the call fails and the local bubble stays
  // intact — same behaviour as the official client.
  revoke: (jid: string, messageID: string) =>
    postBody<{ success: boolean }>(`/api/v2/messages/${encodeURIComponent(messageID)}/revoke`, {
      chat_jid: jid,
    }),
  // edit replaces the text body of a sent message. WhatsApp's edit window is
  // ~15 minutes server-side — past that the bridge call will fail; the UI
  // already hides the Edit action after 15 min to avoid that surprise.
  // Only text messages are editable (the bridge wraps new_text in
  // Conversation, not media captions).
  edit: (jid: string, messageID: string, newText: string) =>
    postBody<{ success: boolean }>(`/api/v2/messages/${encodeURIComponent(messageID)}/edit`, {
      chat_jid: jid,
      new_text: newText,
    }),
  // star / unstar a message — local bookmark only, like WhatsApp's
  // "Starred messages" list. listStarred returns the full message bodies
  // with their chat name attached.
  star: (jid: string, messageID: string) =>
    postBody<{ success: boolean }>(`/api/v2/messages/${encodeURIComponent(messageID)}/star`, {
      chat_jid: jid,
    }),
  unstar: (jid: string, messageID: string) =>
    postBody<{ success: boolean }>(`/api/v2/messages/${encodeURIComponent(messageID)}/unstar`, {
      chat_jid: jid,
    }),
  listStarred: async (): Promise<Message[]> => {
    const res = await fetch('/api/v2/starred')
    if (!res.ok) return []
    return res.json()
  },
  // refreshContactProfile asks the bridge to re-fetch this contact's WA-side
  // identity (verified business name, plain business name, push name, is_business)
  // via GetUserInfo + GetBusinessProfile and upsert it locally. The bridge
  // has a per-JID hourly cooldown so calling on every chat open is cheap.
  // Returns null when the JID has no business identity (404).
  refreshContactProfile: async (jid: string): Promise<Contact | null> => {
    const res = await fetch(
      `/api/v2/contacts/${encodeURIComponent(jid)}/refresh-profile`,
      { method: 'POST' },
    )
    if (res.status === 404) return null
    if (!res.ok) throw new Error(`refresh-profile: ${res.status}`)
    return res.json()
  },
  // forward reposts the message at (fromChat, messageID) into a different
  // chat. Backend currently re-sends the text body with the WA "Forwarded"
  // badge (ContextInfo.IsForwarded=true). Call once per target chat for
  // multi-forward — the UI's share-sheet does exactly that.
  forward: (fromChat: string, messageID: string, toChat: string) =>
    postBody<{ success: boolean; message_id: string }>('/api/v2/forward', {
      from_chat: fromChat,
      message_id: messageID,
      to_chat: toChat,
    }),
  // react adds (or removes, when emoji is "") a reaction to a message — the
  // same /react endpoint the agents already use. WhatsApp treats reactions as
  // self-replacing: posting any emoji clears your previous one on that
  // message, posting "" clears it entirely.
  react: (jid: string, messageID: string, emoji: string) =>
    postBody<{ success: boolean }>('/api/v2/react', {
      chat_jid: jid,
      message_id: messageID,
      emoji,
    }),
  contacts: async (q = ''): Promise<Contact[]> => {
    const res = await fetch('/api/v2/contacts' + (q ? `?q=${encodeURIComponent(q)}` : ''))
    return res.json()
  },
  groups: async (): Promise<Group[]> => {
    const res = await fetch('/api/v2/groups')
    return res.json()
  },
  chatStats: async (): Promise<ChatStat[]> => {
    const res = await fetch('/api/v2/stats/messages')
    return res.json()
  },
  circles: async (): Promise<Circle[]> => {
    const res = await fetch('/api/v2/circles')
    return res.json()
  },
  createCircle: (name: string, color: string) =>
    postBody<Circle>('/api/v2/circles', { name, color }),
  getCircle: async (id: number): Promise<CircleDetail> => {
    const res = await fetch(`/api/v2/circles/${id}`)
    return res.json()
  },
  updateCircle: (
    id: number,
    body: { name: string; color: string; notes?: string; keywords?: string[] },
  ) => putJSON<Circle>(`/api/v2/circles/${id}`, body),
  circleSuggestions: async (id: number): Promise<CircleSuggestions> => {
    const res = await fetch(`/api/v2/circles/${id}/suggestions`)
    return res.json()
  },
  circleContacts: async (id: number): Promise<CircleContact[]> => {
    const res = await fetch(`/api/v2/circles/${id}/contacts`)
    return res.json()
  },
  // circleChats returns the flattened set of chat JIDs belonging to a
  // circle (including its sub-circles), used by Focus Mode's chat-list
  // panel to filter the global chats list down to this circle.
  circleChats: async (id: number): Promise<string[]> => {
    const res = await fetch(`/api/v2/circles/${id}/chats`)
    const data = await res.json()
    return data.chat_jids ?? []
  },
  // exportCircle downloads a .zip of every chat in the circle as plain-text
  // transcripts. Fetched as a blob so the caller can show a progress state.
  exportCircle: async (id: number, filename: string): Promise<void> => {
    const res = await fetch(`/api/v2/circles/${id}/export`)
    if (!res.ok) throw new Error(`Export failed (${res.status})`)
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  },
  tags: async (): Promise<Tag[]> => {
    const res = await fetch('/api/v2/tags')
    return res.json()
  },
  deleteTag: (id: number) => del(`/api/v2/tags/${id}`),
  contactTagsMap: async (): Promise<Record<string, Tag[]>> => {
    const res = await fetch('/api/v2/contacts/tags')
    return res.json()
  },
  assignTag: (jid: string, body: { tag_id?: number; name?: string; color?: string }) =>
    postBody<Tag[]>(`/api/v2/contacts/${encodeURIComponent(jid)}/tags`, body),
  unassignTag: (jid: string, tagId: number) =>
    del(`/api/v2/contacts/${encodeURIComponent(jid)}/tags`, { tag_id: tagId }),

  // --- tasks ---
  tasks: async (
    params: { status?: string; chat?: string; circle?: number; review?: TaskReviewStatus } = {},
  ): Promise<Task[]> => {
    const q = new URLSearchParams()
    if (params.status) q.set('status', params.status)
    if (params.chat) q.set('chat', params.chat)
    if (params.circle) q.set('circle', String(params.circle))
    if (params.review) q.set('review', params.review)
    const res = await fetch('/api/v2/tasks' + (q.toString() ? `?${q}` : ''))
    return res.json()
  },
  reviewTask: (id: number, review: TaskReviewStatus) =>
    postBody<Task>(`/api/v2/tasks/${id}/review`, { review_status: review }),
  clusterCircleTasks: (circleId: number) =>
    postBody<ClusterResult>(`/api/v2/tasks/cluster?circle=${circleId}`, {}),
  createTask: (body: {
    title: string
    assignee_jid?: string
    due_at?: number
    origin_chat_jid?: string
    origin_message_id?: string
    circle_id?: number
  }) => postBody<Task>('/api/v2/tasks', body),
  getTask: async (id: number): Promise<TaskDetail> => {
    const res = await fetch(`/api/v2/tasks/${id}`)
    return res.json()
  },
  updateTask: (
    id: number,
    body: Partial<{
      title: string
      description: string
      status: TaskStatus
      priority: string
      assignee_jid: string
      due_at: number
    }>,
  ) => putJSON<Task>(`/api/v2/tasks/${id}`, body),
  deleteTask: (id: number) => del(`/api/v2/tasks/${id}`),
  linkTaskMessage: (id: number, body: { chat_jid: string; message_id: string; role: string }) =>
    postBody<{ success: boolean }>(`/api/v2/tasks/${id}/messages`, body),
  unlinkTaskMessage: (id: number, body: { chat_jid: string; message_id: string; role: string }) =>
    del(`/api/v2/tasks/${id}/messages`, body),
  addTaskCircle: (id: number, circleId: number) =>
    postBody<{ success: boolean }>(`/api/v2/tasks/${id}/circles`, { circle_id: circleId }),
  extractTasks: (chatJid: string, groupName?: string) =>
    postBody<{ run_id: string }>('/api/v2/tasks/extract', { chat_jid: chatJid, group_name: groupName }),
  cancelRun: (runId: string) => postBody<{ cancelled: boolean }>(`/api/v2/extractions/runs/${runId}/cancel`, {}),
  getRun: async (runId: string): Promise<ExtractionRunState> => {
    const res = await fetch(`/api/v2/extractions/runs/${runId}`)
    return res.json()
  },
  // SSE URL (open with `new EventSource(...)`).
  runStreamURL: (runId: string) => `/api/v2/extractions/runs/${runId}/stream`,
  listExtractions: async (chatJid: string): Promise<ExtractionRun[]> => {
    const res = await fetch('/api/v2/extractions?chat=' + encodeURIComponent(chatJid))
    const data = await res.json()
    return data.runs || []
  },
  listCircleExtractions: async (circleId: number): Promise<ExtractionRun[]> => {
    const res = await fetch('/api/v2/extractions?circle=' + circleId)
    const data = await res.json()
    return data.runs || []
  },
  extractCircleTasks: (circleId: number, name?: string) =>
    postBody<{ run_id: string }>(`/api/v2/circles/${circleId}/extract`, { name }),
  extractionTranscript: async (sessionId: string): Promise<ExtractionStep[]> => {
    const res = await fetch('/api/v2/extractions/transcript?session=' + encodeURIComponent(sessionId))
    const data = await res.json()
    return data.steps || []
  },

  // Entity profiles (purpose descriptions).
  getProfile: async (type: ProfileEntityType, ref: string): Promise<EntityProfile | null> => {
    const q = `type=${type}&ref=${encodeURIComponent(ref)}`
    const res = await fetch('/api/v2/profiles?' + q)
    return res.json()
  },
  saveProfile: (type: ProfileEntityType, ref: string, description: string) =>
    putJSON<EntityProfile>('/api/v2/profiles', { type, ref, description }),
  regenerateProfile: (type: ProfileEntityType, ref: string) =>
    postBody<{ queued: boolean }>('/api/v2/profiles/regenerate', { type, ref }),
  profilesStatus: async (): Promise<ProfilesStatus> => {
    const res = await fetch('/api/v2/profiles/status')
    return res.json()
  },
  startProfiling: () => postBody<{ enabled: boolean }>('/api/v2/profiles/status', {}),

  // Briefings.
  briefingToday: async (): Promise<BriefingRow | null> => {
    const res = await fetch('/api/v2/briefings/today')
    return res.json()
  },
  generateBriefing: () => postBody<BriefingRow>('/api/v2/briefings/generate', {}),
  listBriefings: async (): Promise<BriefingRow[]> => {
    const res = await fetch('/api/v2/briefings')
    return res.json()
  },

  draftReplies: async (
    jid: string,
  ): Promise<{ drafts: { text: string; style?: string; reason?: string }[] }> => {
    const res = await fetch(`/api/v2/chats/${encodeURIComponent(jid)}/draft-replies`, {
      method: 'POST',
    })
    if (!res.ok) throw new Error(`${res.status}`)
    return res.json()
  },

  contactDashboard: async (jid: string): Promise<DashContact> => {
    const res = await fetch(`/api/v2/contacts/${encodeURIComponent(jid)}/dashboard`)
    return res.json()
  },
  groupDashboard: async (jid: string): Promise<DashGroup> => {
    const res = await fetch(`/api/v2/groups/${encodeURIComponent(jid)}/dashboard`)
    return res.json()
  },

  search: async (q: string): Promise<{ q: string; hits: SearchHit[] }> => {
    const res = await fetch('/api/v2/search?q=' + encodeURIComponent(q))
    return res.json()
  },

  autoExtractStatus: async (): Promise<AutoExtractStatus> => {
    const res = await fetch('/api/v2/extractions/auto')
    return res.json()
  },
  autoExtractSet: (enabled?: boolean, intervalHours?: number) =>
    postBody<AutoExtractStatus>('/api/v2/extractions/auto', {
      enabled,
      interval_hours: intervalHours,
    }),

  mediaUnderstandingStatus: async (): Promise<MediaUnderstandingStatus> => {
    const res = await fetch('/api/v2/media/understanding')
    return res.json()
  },
  mediaUnderstandingSet: (audio?: boolean, image?: boolean) =>
    postBody<MediaUnderstandingStatus>('/api/v2/media/understanding', {
      audio_enabled: audio,
      image_enabled: image,
    }),

  // Hidden chats / lock.
  hiddenStatus: async (): Promise<HiddenStatus> => {
    const res = await fetch('/api/v2/hidden/status')
    return res.json()
  },
  hiddenPinSetup: (pin: string, currentPin?: string) =>
    postBody<{ pin_set: boolean }>('/api/v2/hidden/pin/setup', {
      pin,
      current_pin: currentPin,
    }),
  // Returns a pin_passed_token used for the subsequent WebAuthn assertion/registration.
  hiddenUnlockPin: (pin: string) =>
    postBody<{ pin_passed_token: string; webauthn_registered: boolean }>(
      '/api/v2/hidden/unlock/pin',
      { pin },
    ),
  // Lock the session.
  hiddenLock: () => postBody<{ locked: boolean }>('/api/v2/hidden/lock', {}),
  // Per-chat unlock: returns a WebAuthn challenge for ONE hidden chat (no
  // PIN required). The follow-up verify mints a chat-scoped token.
  hiddenChatOptions: (chatJID: string) =>
    postBody<{ publicKey: any; session_id: string }>(
      '/api/v2/hidden/webauthn/chat/options',
      { chat_jid: chatJID },
    ),
  hiddenChatVerify: (sessionID: string, credential: any) =>
    postBody<{ unlock_token: string; chat_jid: string; ttl_seconds: number }>(
      '/api/v2/hidden/webauthn/chat/verify',
      { session_id: sessionID, credential },
    ),
  hideChatPreview: async (jid: string): Promise<HideChatPreview> => {
    const res = await fetch(`/api/v2/chats/${encodeURIComponent(jid)}/hide-preview`)
    return res.json()
  },
  hideChat: (jid: string) =>
    postBody<HideChatResult>(`/api/v2/chats/${encodeURIComponent(jid)}/hide`, {}),
  unhideChat: (jid: string) =>
    postBody<{ jid: string; hidden: boolean }>(`/api/v2/chats/${encodeURIComponent(jid)}/unhide`, {}),

  // Backend handler accepts action: archive|unarchive|pin|unpin|mute|unmute|read|unread
  chatAction: (jid: string, action: string, duration?: number) =>
    postBody<{ success: boolean }>(`/api/v2/chats/${encodeURIComponent(jid)}/action`, {
      action,
      duration,
    }),
  // chatDisappearing sets (or clears) the disappearing-messages timer for this
  // chat. timer is in seconds — WA only accepts 0 (off), 86400 (24h),
  // 604800 (7 days), 7776000 (90 days); any other value is rejected by
  // whatsmeow upstream. Round-trip both writes via PUT to the bridge.
  chatDisappearing: (jid: string, timer: number) =>
    fetch(`/api/v2/chats/${encodeURIComponent(jid)}/disappearing`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ timer }),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to set disappearing timer')
      return r.json() as Promise<{ success: boolean }>
    }),
  // sendContact ships one of the user's contacts as a WA ContactMessage
  // (vCard share). Bridge looks up the contact in its local DB to build the
  // vCard body — caller only passes the contact's JID, no name plumbing.
  sendContact: (jid: string, contactJID: string) =>
    postBody<{ success: boolean; message_id: string; timestamp: number; display_name: string }>(
      '/api/v2/send-contact',
      { jid, contact_jid: contactJID },
    ),
  // sendLocation fires a WA LocationMessage — the "📍" share-where-I-am
  // gesture every WA user knows. name + address are optional (render as the
  // location bubble's title + subtitle in WA); coordinates are required.
  sendLocation: (jid: string, lat: number, lng: number, name?: string, address?: string) =>
    postBody<{ success: boolean; message_id: string; timestamp: number }>(
      '/api/v2/send-location',
      { jid, latitude: lat, longitude: lng, name, address },
    ),
  // recentStickers returns the most-recent unique sticker paths the bridge
  // has stored — drives the composer's sticker tray. Each row is a
  // bridge-relative path the tray can render with mediaURL() and pipe
  // straight back into /send with sticker:true to re-send.
  recentStickers: async (limit = 60): Promise<RecentSticker[]> => {
    const res = await fetch('/api/v2/stickers/recent?limit=' + limit)
    if (!res.ok) return []
    return res.json()
  },
  // newsletters returns every WA "channel" (newsletter) the current user
  // follows. Each row carries the verification badge (VERIFIED accounts get
  // a green check), subscriber count, and the user's role (admin/subscriber).
  newsletters: async (): Promise<Newsletter[]> => {
    const res = await fetch('/api/v2/newsletters')
    if (!res.ok) return []
    return res.json()
  },
  // newsletterFollow / newsletterUnfollow add or drop a subscription. The
  // bridge wires both to whatsmeow's Follow / UnfollowNewsletter, then
  // updates the local mirror so the list reflects state immediately.
  newsletterFollow: (jid: string) =>
    postBody<{ success: boolean }>(`/api/v2/newsletters/${encodeURIComponent(jid)}/follow`, {}),
  newsletterUnfollow: (jid: string) =>
    postBody<{ success: boolean }>(`/api/v2/newsletters/${encodeURIComponent(jid)}/unfollow`, {}),
  // newsletterMute toggles do-not-disturb on one channel — same shape as
  // chat mute but lives on the newsletter handler.
  newsletterMute: (jid: string, mute: boolean) =>
    postBody<{ success: boolean }>(`/api/v2/newsletters/${encodeURIComponent(jid)}/mute`, { mute }),
  // linkedDevices returns every device JID currently paired to this account
  // (WA Settings → Linked devices). Resolved upstream via GetUserInfo.
  // Includes a `current` JID so the UI can flag which row is *this* session.
  linkedDevices: async (): Promise<LinkedDevicesResponse> => {
    const res = await fetch('/api/v2/devices')
    if (!res.ok) return { current: '', devices: [] }
    return res.json()
  },
  // selfAbout reads / writes the current user's "About" line — the short bio
  // (e.g. "Available", "At work, ping me later") shown under your name in
  // profile cards. Bridge resolves it via GetUserInfo against the connected
  // device's own JID.
  selfAbout: async (): Promise<string> => {
    const res = await fetch('/api/v2/status/about')
    if (!res.ok) return ''
    const body = (await res.json()) as { text?: string }
    return body.text || ''
  },
  setSelfAbout: (text: string) =>
    fetch('/api/v2/status/about', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to save About')
      return r.json() as Promise<{ success: boolean }>
    }),
  // privacy returns the user's current privacy settings — exactly what WA's
  // "Settings → Privacy" panel shows. Whatsmeow's `PrivacySettings` struct
  // is serialised with PascalCase fields, so we expose the same shape here.
  // Every field is one of a small enum of strings — see the canonical list:
  //
  //   LastSeen / Status / Profile / GroupAdd:
  //     "all" | "contacts" | "contact_blacklist" | "none"
  //   ReadReceipts:  "all" | "none"
  //   Online:        "all" | "match_last_seen"
  //   CallAdd:       "all" | "known"
  //   Messages:      "all" | "contacts"
  //
  // The bridge falls back to "" for any setting WA hasn't yet synced — the
  // UI renders those as a neutral "Default" so we don't lie about what's
  // actually applied server-side.
  privacy: async (): Promise<PrivacySettings> => {
    const res = await fetch('/api/v2/privacy')
    if (!res.ok) throw new Error('Failed to load privacy settings')
    return res.json()
  },
  // setPrivacy writes one setting. setting names are the whatsmeow keys
  // (last / online / status / profile / readreceipts / groupadd / calladd /
  // messages / stickers / defense); value is one of the enums above.
  setPrivacy: (setting: string, value: string) =>
    fetch('/api/v2/privacy', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ setting, value }),
    }).then((r) => {
      if (!r.ok) throw new Error('Failed to save privacy setting')
      return r.json() as Promise<{ success: boolean }>
    }),
  // blocklist returns the set of JIDs the current user has blocked. The
  // bridge wraps whatsmeow's GetBlocklist which returns `{ DHash, JIDs }`;
  // we just hand back the JID list — DHash is only useful for diffing on
  // the server. Used by Contact info to decide Block / Unblock label.
  blocklist: async (): Promise<string[]> => {
    const res = await fetch('/api/v2/blocklist')
    if (!res.ok) return []
    const body = (await res.json()) as { JIDs?: string[] }
    return body.JIDs || []
  },
  // blockContact blocks or unblocks `jid`. WA hides everything in both
  // directions while blocked (status, presence, profile photo, messages).
  // The bridge POSTs to /blocklist with action 'block' | 'unblock'.
  blockContact: (jid: string, action: 'block' | 'unblock') =>
    postBody<{ success: boolean }>('/api/v2/blocklist', { jid, action }),
  removeTaskCircle: (id: number, circleId: number) =>
    del(`/api/v2/tasks/${id}/circles`, { circle_id: circleId }),
  deleteCircle: (id: number) => del(`/api/v2/circles/${id}`),
  addCircleMember: (id: number, member_type: MemberType, member_ref: string) =>
    postBody<{ success: boolean }>(`/api/v2/circles/${id}/members`, { member_type, member_ref }),
  removeCircleMember: (id: number, member_type: MemberType, member_ref: string) =>
    del(`/api/v2/circles/${id}/members`, { member_type, member_ref }),
  recommendations: async (limit = 5): Promise<RecsResponse> => {
    const res = await fetch(`/api/v2/circles/recommendations?limit=${limit}`)
    return res.json()
  },
  dismissRecommendation: (id: string) =>
    postBody<{ success: boolean }>('/api/v2/circles/recommendations/dismiss', { id }),
  restoreRecommendation: (id: string) =>
    postBody<{ success: boolean }>('/api/v2/circles/recommendations/restore', { id }),
  circlesForMember: async (type: 'group' | 'contact', ref: string): Promise<Circle[]> => {
    const res = await fetch(
      `/api/v2/circles/for-member?type=${type}&ref=${encodeURIComponent(ref)}`,
    )
    return res.json()
  },
}

// Human labels for the history-period presets.
export const HISTORY_LABELS: Record<string, string> = {
  '3months': '3 months',
  '1year': '1 year',
  everything: 'Everything',
}

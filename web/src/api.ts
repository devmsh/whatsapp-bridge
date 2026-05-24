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
  last_message?: ChatPreview
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
}

export interface Contact {
  jid: string
  lid?: string
  phone?: string
  name: string
  push_name?: string
  business_name?: string
  is_business?: boolean
}

export interface Group {
  jid: string
  name: string
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
  messages: async (jid: string, limit = 100): Promise<Message[]> => {
    const res = await fetch(
      `/api/v2/messages?chat_jid=${encodeURIComponent(jid)}&limit=${limit}`,
    )
    return res.json()
  },
  send: (jid: string, message: string) =>
    postBody<{ success: boolean; message_id: string; timestamp: number }>('/api/v2/send', {
      jid,
      message,
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

import type { Chat, ChatPreview, Contact, Group } from '../api'

// isGroup / isStatus / isNewsletter detect chat kind from the JID suffix,
// because the chat_type column is not populated.
export const isGroup = (jid: string) => jid.endsWith('@g.us')
export const isStatus = (jid: string) => jid === 'status@broadcast'
export const isNewsletter = (jid: string) => jid.endsWith('@newsletter')

// jidUser returns the part before '@' (phone number or group id).
export const jidUser = (jid: string) => jid.split('@')[0]

// mediaURL converts a stored media_path ("store/images/x.jpg") into a URL the
// bridge serves ("/api/v2/media/images/x.jpg"). The default media dir is "store".
export function mediaURL(path?: string): string | null {
  if (!path) return null
  return '/api/v2/media/' + path.replace(/^store\//, '')
}

// looksLikeName returns false for masked numbers ("+966∙∙∙54"), raw JIDs, and
// bare digits — values we'd rather replace with a real contact name.
function looksLikeName(s: string): boolean {
  if (!s) return false
  if (s.includes('@')) return false
  if (s.includes('∙') || s.includes('•')) return false
  if (/^\+?\d[\d\s-]*$/.test(s)) return false
  return true
}

// buildNameMap indexes display names by JID: group names from the groups table,
// and contacts by JID and by phone. Group and contact JIDs never collide
// (@g.us vs @s.whatsapp.net), so one map serves both.
export function buildNameMap(contacts: Contact[], groups: Group[] = []): Map<string, string> {
  const m = new Map<string, string>()
  for (const g of groups) {
    if (g.name) m.set(g.jid, g.name)
  }
  for (const c of contacts) {
    const name = c.name || c.push_name || c.business_name || ''
    if (!name) continue
    if (c.jid) m.set(c.jid, name)
    if (c.phone) m.set(c.phone + '@s.whatsapp.net', name)
  }
  return m
}

// chatTitle resolves the best display name for a chat.
export function chatTitle(chat: Chat, names: Map<string, string>): string {
  if (isStatus(chat.jid)) return 'Status updates'
  const mapped = names.get(chat.jid)
  if (isGroup(chat.jid)) {
    if (mapped) return mapped
    return looksLikeName(chat.name) ? chat.name : 'Group · ' + jidUser(chat.jid).slice(-4)
  }
  if (mapped) return mapped
  if (looksLikeName(chat.name)) return chat.name
  return '+' + jidUser(chat.jid)
}

// senderTitle resolves a sender name inside a group bubble.
export function senderTitle(
  sender: string,
  senderName: string,
  pushName: string,
  names: Map<string, string>,
): string {
  return names.get(sender) || (looksLikeName(senderName) ? senderName : '') || pushName || '+' + jidUser(sender)
}

const MEDIA_ICON: Record<string, string> = {
  image: '📷',
  video: '🎥',
  voice_note: '🎤',
  audio: '🎵',
  document: '📄',
  sticker: '🌟',
}
const MEDIA_WORD: Record<string, string> = {
  image: 'Photo',
  video: 'Video',
  voice_note: 'Voice message',
  audio: 'Audio',
  document: 'Document',
  sticker: 'Sticker',
}

// previewText builds the chat-list second line, like WhatsApp: media shows an
// icon + label (or its caption), and group messages are prefixed by the sender.
export function previewText(lm: ChatPreview, names: Map<string, string>): string {
  let body: string
  if (lm.is_deleted) {
    body = '🚫 deleted message'
  } else if (lm.media_type) {
    const icon = MEDIA_ICON[lm.media_type] || '📎'
    const showCaption = lm.media_caption && lm.media_type !== 'sticker' && lm.media_type !== 'voice_note'
    body = `${icon} ${showCaption ? lm.media_caption : MEDIA_WORD[lm.media_type] || 'Media'}`
  } else {
    body = lm.content || ''
  }
  if (lm.is_from_me) return 'You: ' + body
  if (lm.is_group) {
    const sn =
      names.get(lm.sender) || (looksLikeName(lm.sender_name) ? lm.sender_name : '') || lm.push_name
    const short = sn ? sn.split(' ')[0] : ''
    return short ? `${short}: ${body}` : body
  }
  return body
}

// initial returns one uppercase letter for an avatar placeholder.
export function initial(title: string): string {
  const t = title.replace(/^\+/, '').trim()
  return (t[0] || '?').toUpperCase()
}

// --- time helpers ---

const pad = (n: number) => (n < 10 ? '0' + n : '' + n)

export function clockTime(ts: number): string {
  const d = new Date(ts * 1000)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function dayLabel(ts: number): string {
  const d = new Date(ts * 1000)
  const today = new Date()
  const yesterday = new Date()
  yesterday.setDate(today.getDate() - 1)
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  if (sameDay(d, today)) return 'Today'
  if (sameDay(d, yesterday)) return 'Yesterday'
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

// chatListTime: short relative label for the chat list (time today, else date).
export function chatListTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  const today = new Date()
  if (
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate()
  ) {
    return clockTime(ts)
  }
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

// humanSize formats a byte count.
export function humanSize(bytes?: number): string {
  if (!bytes) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let n = bytes
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}

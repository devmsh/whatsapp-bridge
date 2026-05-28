import type { Chat, ChatPreview, Contact, Group } from '../api'
import { pickTokenFor } from '../hidden'

// isGroup / isStatus / isNewsletter detect chat kind from the JID suffix,
// because the chat_type column is not populated.
export const isGroup = (jid: string) => jid.endsWith('@g.us')
export const isStatus = (jid: string) => jid === 'status@broadcast'
export const isNewsletter = (jid: string) => jid.endsWith('@newsletter')

// jidUser returns the part before '@' (phone number or group id).
export const jidUser = (jid: string) => jid.split('@')[0]

// mediaURL converts a stored media_path ("store/images/x.jpg") into a URL the
// bridge serves ("/api/v2/media/images/x.jpg"). The default media dir is
// "store".
//
// When chatJID is supplied and we hold an unlock token for it (either global
// or per-chat), the token is appended as ?unlock=… — <audio>/<img> tags
// can't send a custom header, so the media endpoint accepts the token via
// query string as a fallback. Without this a hidden chat's voice notes
// would silently fail to play after a per-chat fingerprint unlock.
export function mediaURL(path?: string, chatJID?: string): string | null {
  if (!path) return null
  const base = '/api/v2/media/' + path.replace(/^store\//, '')
  if (!chatJID) return base
  const tok = pickTokenFor(chatJID)
  if (!tok) return base
  return base + (base.includes('?') ? '&' : '?') + 'unlock=' + encodeURIComponent(tok)
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
// and contacts by JID, by phone, and by LID. Group and contact JIDs never
// collide (@g.us vs @s.whatsapp.net vs @lid), so one map serves all of them.
export function buildNameMap(contacts: Contact[], groups: Group[] = []): Map<string, string> {
  const m = new Map<string, string>()
  for (const g of groups) {
    if (g.name) m.set(g.jid, g.name)
  }
  for (const c of contacts) {
    const name = c.name || c.verified_name || c.business_name || c.push_name || ''
    if (!name) continue
    if (c.jid) m.set(c.jid, name)
    if (c.phone) m.set(c.phone + '@s.whatsapp.net', name)
    if (c.lid) m.set(c.lid + '@lid', name)
  }
  return m
}

// MentionEntry: a JID that's openable as a DM (always phone form), plus a
// display name. `unknown` is true when nothing is in the contact book.
export type MentionEntry = { jid: string; name: string; unknown: boolean }

// buildMentionIndex maps the bare digit identifier inside a "@<digits>"
// mention to the contact's phone-based JID + display name. WhatsApp emits
// mention identifiers as LIDs ("@157419191689245"), so we index by:
//   - the contact's LID digits (when set)
//   - the contact's phone digits
// Both point to the SAME phone-based JID, which is what openChat needs to
// land in a DM.
//
// IMPORTANT: the contacts table also contains junk rows whose `jid` ends in
// `@lid` (or `:N@lid`). On those rows `phone` actually holds the LID digits,
// not a real phone number — using them would map an LID back to itself and
// open the wrong (or non-existent) chat. So we trust phone/LID-digit
// mappings ONLY from rows with a phone-form JID. Junk rows are skipped.
export function buildMentionIndex(contacts: Contact[]): Map<string, MentionEntry> {
  const m = new Map<string, MentionEntry>()
  for (const c of contacts) {
    if (!c.jid || !c.jid.endsWith('@s.whatsapp.net')) continue
    const name = c.name || c.verified_name || c.business_name || c.push_name || ''
    const phoneJID = c.jid // already phone-form
    const entry: MentionEntry = { jid: phoneJID, name, unknown: !name }
    if (c.lid) {
      const digits = c.lid.replace('@lid', '').split(':')[0]
      if (digits) m.set(digits, entry)
    }
    if (c.phone) m.set(c.phone, entry)
    // Also index by the JID's user portion (the phone digits before "@").
    const jidUserPart = c.jid.split('@')[0].split(':')[0]
    if (jidUserPart) m.set(jidUserPart, entry)
  }
  return m
}

// resolveMention looks up a "@<digits>" token. Falls back to a phone-form JID
// + "+<digits>" label when the contact isn't in the book (so a click still
// tries to open a DM with that number).
export function resolveMention(digits: string, index: Map<string, MentionEntry>): MentionEntry {
  const hit = index.get(digits)
  if (hit) return hit
  return { jid: digits + '@s.whatsapp.net', name: '+' + digits, unknown: true }
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

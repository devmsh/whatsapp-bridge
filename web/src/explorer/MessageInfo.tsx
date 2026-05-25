import { useEffect, useMemo, useState } from 'react'
import { api, type Message, type MessageReceipt } from '../api'
import { chatListTime, senderTitle, type MentionEntry } from './format'
import { ChatAvatar } from './ChatAvatar'

// MessageInfo is the "Info" overlay WA pops when you long-press one of your
// own messages and pick Message info. Shows a small preview of the message
// at the top and then two grouped lists of recipients: who has read it
// (highest type ≥ read) and who has only received it (delivered but not
// yet read). For DMs both lists are at most one entry; for groups they're
// per-participant.
//
// We don't render a "not yet delivered" section: the bridge only stores
// rows that actually arrived, so absence-of-row means absence-of-data —
// not the same as "delivered to nobody". Same shorthand WA uses.
export function MessageInfo({
  msg,
  nameMap,
  onClose,
}: {
  msg: Message
  nameMap: Map<string, string>
  onClose: () => void
}) {
  const [receipts, setReceipts] = useState<MessageReceipt[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api
      .messageReceipts(msg.chat_jid, msg.id)
      .then((r) => {
        if (!cancelled) setReceipts(r || [])
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load receipts')
      })
    return () => {
      cancelled = true
    }
  }, [msg.id, msg.chat_jid])

  // Esc closes — same gesture every other overlay in the explorer uses.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Bucket the raw receipts: per-recipient highest type wins. A user who
  // both delivered AND read should appear only in "Read by" with the
  // read-timestamp; the lesser delivered-event is implied.
  const { readBy, deliveredOnly } = useMemo(() => {
    type Best = { sender: string; type: 'read' | 'played' | 'delivered'; ts: number }
    const best = new Map<string, Best>()
    const rank = (t: string) => (t === 'played' ? 3 : t === 'read' ? 2 : 1)
    for (const r of receipts || []) {
      const t = (r.receipt_type || 'delivered') as Best['type']
      const cur = best.get(r.sender_jid)
      if (!cur || rank(t) > rank(cur.type)) {
        best.set(r.sender_jid, { sender: r.sender_jid, type: t, ts: r.timestamp })
      } else if (rank(t) === rank(cur.type) && r.timestamp > cur.ts) {
        cur.ts = r.timestamp
      }
    }
    const readBy: Best[] = []
    const deliveredOnly: Best[] = []
    for (const b of best.values()) {
      if (b.type === 'read' || b.type === 'played') readBy.push(b)
      else deliveredOnly.push(b)
    }
    // Newest first inside each bucket — same as WA's own ordering.
    readBy.sort((a, b) => b.ts - a.ts)
    deliveredOnly.sort((a, b) => b.ts - a.ts)
    return { readBy, deliveredOnly }
  }, [receipts])

  // Preview text for the header card — same shorthand the chat list uses.
  const previewText =
    msg.content ||
    msg.media_caption ||
    (msg.media_type === 'image'
      ? '📷 Photo'
      : msg.media_type === 'video'
        ? '🎥 Video'
        : msg.media_type === 'voice_note'
          ? '🎤 Voice message'
          : msg.media_type === 'audio'
            ? '🎵 Audio'
            : msg.media_type === 'document'
              ? '📄 Document'
              : msg.media_type === 'sticker'
                ? '🌟 Sticker'
                : '')

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/70 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[80vh] w-[420px] max-w-[92vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Message info</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>
        {/* Message preview card — tight emerald bubble so the user knows
            which message they're inspecting. Single-line ellipsis. */}
        <div className="border-b border-neutral-800 px-4 py-3">
          <div className="rounded-2xl bg-emerald-700/40 px-3 py-2 text-sm text-neutral-100">
            <div dir="auto" className="line-clamp-2 break-words">
              {previewText || <span className="italic text-neutral-400">No content</span>}
            </div>
            <div className="mt-1 text-right text-[10px] text-neutral-400">
              {chatListTime(msg.timestamp)}
            </div>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-2">
          {error && (
            <div className="px-3 py-4 text-center text-xs text-red-400">{error}</div>
          )}
          {!error && receipts === null && (
            <div className="px-3 py-6 text-center text-xs text-neutral-500">Loading…</div>
          )}
          {!error && receipts !== null && receipts.length === 0 && (
            <div className="px-3 py-6 text-center text-xs text-neutral-500">
              No receipts yet — the message hasn't been delivered to anyone we can see.
            </div>
          )}

          <Bucket
            title="Read by"
            icon="✓✓"
            iconClass="text-sky-300"
            entries={readBy}
            nameMap={nameMap}
            chatJID={msg.chat_jid}
          />
          <Bucket
            title="Delivered to"
            icon="✓✓"
            iconClass="text-neutral-400"
            entries={deliveredOnly}
            nameMap={nameMap}
            chatJID={msg.chat_jid}
          />
        </div>
      </div>
    </div>
  )
}

function Bucket({
  title,
  icon,
  iconClass,
  entries,
  nameMap,
}: {
  title: string
  icon: string
  iconClass: string
  entries: { sender: string; ts: number }[]
  nameMap: Map<string, string>
  chatJID: string
}) {
  if (entries.length === 0) return null
  return (
    <div className="mb-3">
      <div className="flex items-center gap-2 px-3 pb-1 pt-2 text-[11px] uppercase tracking-wider text-neutral-500">
        <span className={'text-sm ' + iconClass}>{icon}</span>
        <span>{title}</span>
        <span className="ml-auto tabular-nums">{entries.length}</span>
      </div>
      <ul className="flex flex-col">
        {entries.map((e) => {
          const name = senderTitle(e.sender, '', '', nameMap)
          return (
            <li key={e.sender + e.ts} className="flex items-center gap-3 rounded-lg px-3 py-2 transition hover:bg-neutral-800/60">
              <ChatAvatar jid={e.sender} title={name} size={32} />
              <div className="min-w-0 flex-1">
                <div dir="auto" className="truncate text-sm text-neutral-100">{name}</div>
              </div>
              <div className="shrink-0 text-xs text-neutral-400 tabular-nums">
                {chatListTime(e.ts)}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// Re-export so MessageThread can import without yet another type juggle.
export type { MentionEntry }

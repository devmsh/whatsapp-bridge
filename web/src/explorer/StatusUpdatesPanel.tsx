import { useEffect, useMemo, useState } from 'react'
import { api, type Message } from '../api'
import { ChatAvatar } from './ChatAvatar'

// StatusUpdatesPanel is the WA "Status" tab in modal form. WA mobile groups
// the last 24h of contacts' status updates by sender — one row per person
// with their latest preview, tap to expand. Bridge stores every status
// update as a message in the status@broadcast pseudo-chat, so the data is
// already there; we just group on the client.
//
// 24h cutoff isn't strict — WA hides anything older but the bridge keeps
// the history. We surface the past 24h to mirror WA mobile, but also offer
// a "show older" toggle for archival peeks.
export function StatusUpdatesPanel({ onClose }: { onClose: () => void }) {
  const [messages, setMessages] = useState<Message[] | null>(null)
  const [error, setError] = useState('')
  const [showOlder, setShowOlder] = useState(false)

  useEffect(() => {
    api
      .messages('status@broadcast', 500)
      .then((msgs) => setMessages(msgs || []))
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load statuses'))
  }, [])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  type StatusRow = {
    sender: string
    name: string
    latest: Message
    count: number
  }
  const grouped = useMemo<StatusRow[]>(() => {
    if (!messages) return []
    const cutoff = Math.floor(Date.now() / 1000) - 24 * 3600
    const filtered = showOlder ? messages : messages.filter((m) => m.timestamp >= cutoff)
    const bySender = new Map<string, StatusRow>()
    for (const m of filtered) {
      const sender = m.sender
      if (!sender) continue
      const existing = bySender.get(sender)
      if (!existing) {
        bySender.set(sender, {
          sender,
          name: m.sender_name || m.push_name || '+' + (sender.split('@')[0] || '').split(':')[0],
          latest: m,
          count: 1,
        })
      } else {
        existing.count++
        if (m.timestamp > existing.latest.timestamp) existing.latest = m
      }
    }
    return [...bySender.values()].sort((a, b) => b.latest.timestamp - a.latest.timestamp)
  }, [messages, showOlder])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[480px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <div className="flex items-baseline gap-2">
            <h2 className="text-sm font-semibold text-neutral-100">Status updates</h2>
            {grouped.length > 0 && (
              <span className="rounded-full bg-neutral-800 px-2 py-0.5 text-[10px] font-medium text-neutral-400">
                {grouped.length} {grouped.length === 1 ? 'person' : 'people'}
              </span>
            )}
          </div>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex-1 overflow-y-auto">
          {error && (
            <div className="m-4 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}
          {!error && messages === null && (
            <div className="py-10 text-center text-xs text-neutral-500">Loading statuses…</div>
          )}
          {messages !== null && grouped.length === 0 && (
            <div className="py-10 text-center text-xs text-neutral-600">
              {showOlder
                ? 'No status updates in the archive.'
                : 'No status updates in the last 24 hours.'}
            </div>
          )}
          {grouped.map((row) => (
            <div
              key={row.sender}
              className="flex items-start gap-3 border-b border-neutral-900 px-4 py-3"
            >
              {/* Emerald ring around the avatar marks "has a recent status",
                  matching WA mobile's exact convention. */}
              <div className="rounded-full p-[2px] ring-2 ring-emerald-500/70">
                <ChatAvatar jid={row.sender} title={row.name} size={42} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                  <span dir="auto" className="truncate text-sm font-medium text-neutral-100">
                    {row.name}
                  </span>
                  <span className="shrink-0 text-[11px] text-neutral-500">
                    {fmtRel(row.latest.timestamp)}
                  </span>
                </div>
                <div dir="auto" className="line-clamp-2 text-[12px] text-neutral-400">
                  {row.latest.content ||
                    row.latest.media_caption ||
                    statusMediaWord(row.latest.media_type) ||
                    '(no preview)'}
                </div>
                <div className="mt-0.5 text-[10px] text-neutral-600">
                  {row.count} update{row.count === 1 ? '' : 's'}
                </div>
              </div>
            </div>
          ))}
        </div>

        <footer className="flex items-center justify-between border-t border-neutral-800 px-4 py-2">
          <span className="text-[11px] text-neutral-500">
            Status updates auto-expire after 24h on WhatsApp.
          </span>
          <button
            onClick={() => setShowOlder((v) => !v)}
            className="rounded-md border border-neutral-700 px-2.5 py-1 text-[11px] text-neutral-300 transition hover:bg-neutral-800"
          >
            {showOlder ? 'Hide older' : 'Show older'}
          </button>
        </footer>
      </div>
    </div>
  )
}

// statusMediaWord is the one-word preview a status row shows when the
// message has no caption. Matches WA's status-list shorthand exactly.
function statusMediaWord(t?: string): string {
  switch (t) {
    case 'image': return '📷 Photo'
    case 'video': return '🎬 Video'
    case 'voice_note':
    case 'audio': return '🎤 Audio'
    default: return ''
  }
}

// fmtRel renders "now" / "12m" / "3h" — short relative-time string. WA
// status timestamps are always recent (24h max), so we don't bother with a
// date fallback.
function fmtRel(ts: number): string {
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - ts)
  if (diff < 60) return 'now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}

import { useEffect, useState } from 'react'
import { api, type Message } from '../api'
import { clockTime } from './format'

// StarredPanel is the global "Starred messages" modal — equivalent of
// WhatsApp's three-dots → Starred messages. Lists every starred message
// across all (non-hidden) chats, newest-first, with chat label + sender +
// snippet. Clicking a row jumps to that chat. Each row also has a tiny
// unstar X for quick cleanup.
export function StarredPanel({
  onOpenChat,
  onClose,
}: {
  onOpenChat: (jid: string) => void
  onClose: () => void
}) {
  const [items, setItems] = useState<Message[] | null>(null)
  const [error, setError] = useState('')

  // Load on mount + reload whenever any inline unstar succeeds.
  async function reload() {
    setError('')
    try {
      const list = await api.listStarred()
      setItems(list)
    } catch (e) {
      setItems([])
      setError(e instanceof Error ? e.message : 'Failed to load')
    }
  }
  useEffect(() => {
    void reload()
  }, [])

  // Esc closes — same dismissal contract as the other modals.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function unstar(m: Message) {
    // Optimistic — drop from the list first, fall back if the API rejects.
    setItems((prev) => (prev || []).filter((x) => x.id !== m.id))
    try {
      await api.unstar(m.chat_jid, m.id)
    } catch {
      void reload()
    }
  }

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex h-[80vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl bg-neutral-900 shadow-xl ring-1 ring-neutral-800"
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-neutral-100">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" className="text-amber-300">
              <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
            </svg>
            Starred messages
            {items && (
              <span className="text-xs font-normal text-neutral-500">
                {items.length === 0 ? 'none' : items.length}
              </span>
            )}
          </h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded-full text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-100"
          >
            ✕
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {items === null ? (
            <div className="py-10 text-center text-xs text-neutral-500">Loading…</div>
          ) : items.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-neutral-500">
              {error ? (
                <span className="text-red-400">{error}</span>
              ) : (
                <>
                  Nothing starred yet.
                  <div className="mt-1 text-xs text-neutral-600">
                    Hover any message and click the ☆ to bookmark it.
                  </div>
                </>
              )}
            </div>
          ) : (
            items.map((m) => (
              <StarredRow
                key={m.chat_jid + '|' + m.id}
                msg={m}
                onOpen={() => {
                  onOpenChat(m.chat_jid)
                  onClose()
                }}
                onUnstar={() => void unstar(m)}
              />
            ))
          )}
        </div>
      </div>
    </div>
  )
}

// StarredRow renders one entry: chat label on top, sender + snippet below,
// time on the right. Whole row is the "open chat" target; the unstar X has
// stopPropagation so it doesn't navigate.
function StarredRow({
  msg,
  onOpen,
  onUnstar,
}: {
  msg: Message
  onOpen: () => void
  onUnstar: () => void
}) {
  const chatLabel = msg.chat_name || msg.chat_jid
  // Sender label: 'You' for outgoing; sender_name / push_name / '+phone'
  // fallback. Avoid empty strings so the row never reads "  : Hello".
  const sender = msg.is_from_me
    ? 'You'
    : msg.sender_name || msg.push_name || ('+' + (msg.sender || '').split('@')[0])
  // Snippet: prefer content, fall back to caption, otherwise a media stub.
  const snippet =
    msg.content?.trim() ||
    msg.media_caption?.trim() ||
    mediaWord(msg.media_type) ||
    'Message'

  return (
    <button
      onClick={onOpen}
      className="flex w-full items-start gap-3 border-b border-neutral-900 px-4 py-3 text-start transition hover:bg-neutral-800/60"
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <div dir="auto" className="truncate text-xs font-semibold text-emerald-300">
            {chatLabel}
          </div>
          <span className="text-[10px] tabular-nums text-neutral-600">
            · {clockTime(msg.starred_at || msg.timestamp)}
          </span>
        </div>
        <div className="mt-0.5 flex items-baseline gap-2 text-sm">
          <span className="shrink-0 text-xs text-neutral-400">{sender}:</span>
          <span dir="auto" className="line-clamp-2 text-neutral-200">
            {snippet}
          </span>
        </div>
      </div>
      <button
        onClick={(e) => {
          e.stopPropagation()
          onUnstar()
        }}
        title="Remove star"
        aria-label="Remove star"
        className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-amber-300 transition hover:bg-amber-500/20"
      >
        <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor">
          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
        </svg>
      </button>
    </button>
  )
}

// mediaWord mirrors the chat-list preview shorthand so a snippet of a pure-
// media message stays meaningful in the list.
function mediaWord(t?: string): string {
  switch (t) {
    case 'image': return '📷 Photo'
    case 'video': return '🎥 Video'
    case 'voice_note': return '🎤 Voice message'
    case 'audio': return '🎵 Audio'
    case 'document': return '📄 Document'
    case 'sticker': return '🌟 Sticker'
    default: return ''
  }
}

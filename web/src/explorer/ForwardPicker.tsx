import { useEffect, useMemo, useState } from 'react'
import { api, type Chat, type Message } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { chatTitle, isGroup, isStatus } from './format'

// ForwardPicker is the multi-select share sheet WhatsApp opens when you tap
// "Forward" on a message. It shows the chat list (most-recent first), a
// search filter, and a checkbox per row. Selecting one or more targets and
// clicking "Forward" fires /api/v2/forward once per target.
//
// Failure handling is partial-success: we send to each target sequentially
// and report 'sent N · failed M'. A failed target doesn't roll the others
// back — official WA behaves the same way.
export function ForwardPicker({
  msg,
  chats,
  nameMap,
  onClose,
}: {
  msg: Message
  chats: Chat[]
  nameMap: Map<string, string>
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [sending, setSending] = useState(false)
  const [status, setStatus] = useState('')

  // Esc closes — same dismissal contract as the lightbox and reply chip.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Build the candidate list once per (chats, query) change. We exclude:
  //  - status broadcast (not a real chat)
  //  - the current chat the message is in (forwarding to itself is rarely
  //    intended; user can still pick it explicitly by typing its name —
  //    we just don't push it to the top)
  //  - hidden chats (their JIDs would leak through the picker otherwise)
  const targets = useMemo(() => {
    const q = query.trim().toLowerCase()
    return chats
      .filter((c) => !isStatus(c.jid) && !c.is_hidden)
      .map((c) => ({
        chat: c,
        title: chatTitle(c, nameMap),
        last: c.last_message_at || 0,
      }))
      .filter(({ title }) => (q ? title.toLowerCase().includes(q) : true))
      .sort((a, b) => b.last - a.last)
      .slice(0, 200)
  }, [chats, query, nameMap])

  function toggle(jid: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(jid)) next.delete(jid)
      else next.add(jid)
      return next
    })
  }

  async function send() {
    if (selected.size === 0 || sending) return
    setSending(true)
    setStatus('')
    let ok = 0
    let fail = 0
    // Sequential so a stuck target doesn't pile up parallel sends. The
    // typical multi-forward is 2–5 chats — sub-second total.
    for (const toJID of selected) {
      try {
        await api.forward(msg.chat_jid, msg.id, toJID)
        ok++
      } catch {
        fail++
      }
    }
    setSending(false)
    if (fail === 0) {
      onClose()
    } else {
      setStatus(`sent ${ok} · failed ${fail}`)
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
        className="flex h-[80vh] w-full max-w-md flex-col overflow-hidden rounded-2xl bg-neutral-900 shadow-xl ring-1 ring-neutral-800"
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Forward to…</h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded-full text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-100"
          >
            ✕
          </button>
        </div>

        <div className="border-b border-neutral-800 px-4 py-2">
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search chats…"
            className="w-full rounded-lg bg-neutral-800 px-3 py-2 text-sm outline-none placeholder:text-neutral-500 focus:ring-1 focus:ring-emerald-600"
          />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {targets.length === 0 ? (
            <div className="py-10 text-center text-xs text-neutral-500">No chats match.</div>
          ) : (
            targets.map(({ chat, title }) => {
              const checked = selected.has(chat.jid)
              return (
                <button
                  key={chat.jid}
                  onClick={() => toggle(chat.jid)}
                  className={
                    'flex w-full items-center gap-3 border-b border-neutral-900 px-4 py-2 text-start transition hover:bg-neutral-800/60 ' +
                    (checked ? 'bg-emerald-500/10' : '')
                  }
                >
                  <ChatAvatar jid={chat.jid} title={title} group={isGroup(chat.jid)} size={32} />
                  <div className="min-w-0 flex-1">
                    <div dir="auto" className="truncate text-sm text-neutral-100">
                      {title}
                    </div>
                    {isGroup(chat.jid) && (
                      <div className="truncate text-[11px] text-neutral-500">Group</div>
                    )}
                  </div>
                  <span
                    className={
                      'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border transition ' +
                      (checked
                        ? 'border-emerald-500 bg-emerald-500 text-neutral-950'
                        : 'border-neutral-600 text-transparent')
                    }
                  >
                    ✓
                  </span>
                </button>
              )
            })
          )}
        </div>

        <div className="flex items-center justify-between border-t border-neutral-800 px-4 py-3">
          <div className="text-xs text-neutral-500">
            {status || (selected.size > 0 ? `${selected.size} selected` : 'Pick one or more chats')}
          </div>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="rounded-lg px-3 py-1.5 text-xs text-neutral-300 transition hover:bg-neutral-800"
            >
              Cancel
            </button>
            <button
              onClick={send}
              disabled={selected.size === 0 || sending}
              className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {sending ? 'Sending…' : `Forward${selected.size > 1 ? ` (${selected.size})` : ''}`}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Contact } from '../api'
import { ChatAvatar } from './ChatAvatar'

// SendContactModal — WA's "Share a contact" attachment. Pops over the
// composer, lets the user search their contacts by name or phone, pick
// exactly one, and ship them to the open chat as a vCard.
//
// Single-select (WA also allows multi-select; one is enough for a clean
// first cycle and matches the most common "send me X's number" gesture).
export function SendContactModal({
  chatJID,
  onClose,
  onSent,
}: {
  chatJID: string
  onClose: () => void
  /** Called once the bridge confirms the send. The SSE round-trip will
   *  light up the real bubble; the caller doesn't need to echo. */
  onSent: () => void
}) {
  const [contacts, setContacts] = useState<Contact[] | null>(null)
  const [q, setQ] = useState('')
  const [sending, setSending] = useState<string | null>(null) // contact JID
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api
      .contacts()
      .then(setContacts)
      .catch(() => setError('Could not load contacts.'))
  }, [])

  useEffect(() => {
    inputRef.current?.focus()
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

  const matches = useMemo(() => {
    if (!contacts) return []
    const needle = q.trim().toLowerCase()
    const out: Contact[] = []
    for (const c of contacts) {
      if (c.is_hidden) continue
      // Skip ourselves — sharing your own card is a separate gesture
      // (WA Settings → Share my contact), not part of this picker.
      const name = c.name || c.business_name || c.push_name || ''
      const phone = c.phone || ''
      if (needle && !name.toLowerCase().includes(needle) && !phone.includes(needle)) continue
      out.push(c)
    }
    out.sort((a, b) =>
      (a.name || a.business_name || a.phone || '').localeCompare(
        b.name || b.business_name || b.phone || '',
      ),
    )
    return out.slice(0, 300)
  }, [contacts, q])

  async function pick(c: Contact) {
    if (sending) return
    setSending(c.jid)
    setError('')
    try {
      await api.sendContact(chatJID, c.jid)
      onSent()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Send failed')
    } finally {
      setSending(null)
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[85] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-[440px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Share contact</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="border-b border-neutral-800 px-4 py-2">
          <input
            ref={inputRef}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search name or number…"
            className="w-full rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-2 text-sm text-neutral-100 outline-none placeholder:text-neutral-600 focus:border-neutral-600"
          />
        </div>

        <div className="flex-1 overflow-y-auto">
          {contacts === null && !error && (
            <div className="py-10 text-center text-xs text-neutral-500">Loading…</div>
          )}
          {contacts !== null && matches.length === 0 && (
            <div className="py-10 text-center text-xs text-neutral-600">
              {q.trim() ? 'No matches.' : 'No contacts.'}
            </div>
          )}
          {matches.map((c) => {
            const name = c.name || c.business_name || c.push_name || '+' + (c.phone || '')
            const sub = c.phone ? '+' + c.phone : ''
            const isBusy = sending === c.jid
            return (
              <button
                key={c.jid}
                onClick={() => void pick(c)}
                disabled={!!sending}
                className="flex w-full items-center gap-3 border-b border-neutral-900 px-4 py-2.5 text-left transition hover:bg-neutral-800/60 disabled:opacity-50"
              >
                <ChatAvatar jid={c.jid} title={name} size={36} />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm text-neutral-100">{name}</div>
                  {sub && <div className="truncate text-[11px] text-neutral-500">{sub}</div>}
                </div>
                {isBusy && (
                  <span className="text-[11px] text-neutral-400">Sending…</span>
                )}
              </button>
            )
          })}
        </div>

        {error && (
          <div className="border-t border-red-900/60 bg-red-950/40 px-4 py-2 text-[11px] text-red-300">
            {error}
          </div>
        )}
      </div>
    </div>
  )
}

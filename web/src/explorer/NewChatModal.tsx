import { useEffect, useMemo, useRef, useState } from 'react'
import type { Contact, DeviceInfo, Group } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { isGroup } from './format'

// NewChatModal is WA's pencil-button compose. The user hits ✏️ in the
// sidebar header, this opens, they search a contact or group by name /
// phone, pick one, and openChat fires for that jid. Universal search at
// the top of the sidebar already supports the same flow as a side
// effect; this gives it a dedicated, obvious entry point — the WA
// gesture every user expects.
//
// We don't fetch anything: contacts + groups come in as props from
// Explorer (which already has them loaded), so the picker is instant.
export function NewChatModal({
  contacts,
  groups,
  selfDevice,
  onClose,
  onPick,
}: {
  contacts: Contact[]
  groups: Group[]
  /** Connected-device info (when known). Drives the "Message yourself"
   *  / Saved Messages row at the top of the picker — pings the user's
   *  own JID, which whatsmeow handles as the WA "message yourself"
   *  feature so it doubles as a personal notepad. */
  selfDevice?: DeviceInfo
  onClose: () => void
  onPick: (jid: string) => void
}) {
  const [q, setQ] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
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

  // Build one flat target list and filter by query — name / phone /
  // business_name all match. Contacts come ahead of groups in the result
  // ordering because that's the WA convention; both are independently
  // sorted alphabetically. Hidden contacts are excluded so the picker
  // never lets a curious user discover them by name.
  //
  // When the query looks like a raw phone number (digits, optional +) we
  // ALSO surface a synthetic "Start chat with +XXX" row at the top so
  // the user can reach a JID that isn't in their contacts yet — same
  // gesture WA mobile uses when you type a number into search.
  const matches = useMemo(() => {
    type Row = {
      kind: 'contact' | 'group' | 'phone' | 'self'
      jid: string
      title: string
      subtitle: string
    }
    const needle = q.trim().toLowerCase()
    const out: Row[] = []
    // Saved Messages / "Message yourself" — top of the list when we know
    // the connected device's JID. WA's recent feature for personal notes:
    // sends to yourself land back via the SSE stream as a self-thread.
    // Skipped when the user has typed a query (the picker becomes a
    // filtered list of matches, and a generic "Saved messages" wouldn't
    // match anyway unless they typed "saved" specifically).
    if (selfDevice?.jid && !needle) {
      // Strip the device suffix (`:NN`) — the non-suffixed JID is the
      // "message yourself" target whatsmeow accepts.
      const self = selfDevice.jid.replace(/:\d+@/, '@')
      out.push({
        kind: 'self',
        jid: self,
        title: '⭐ Saved messages',
        subtitle: 'Message yourself · keep notes, links, anything',
      })
    }
    // Phone shortcut — sits above contacts so a typed number always has a
    // one-click path even when contacts happen to share digits.
    const phoneMatch = q.trim().match(/^\+?(\d{6,15})$/)
    if (phoneMatch) {
      const digits = phoneMatch[1]
      const jid = digits + '@s.whatsapp.net'
      // Only add the shortcut if no existing contact already matches this
      // number — otherwise we'd duplicate that contact's row.
      const alreadyKnown = contacts.some((c) => c.phone === digits || c.jid === jid)
      if (!alreadyKnown) {
        out.push({
          kind: 'phone',
          jid,
          title: 'Start chat with +' + digits,
          subtitle: 'Not in your contacts',
        })
      }
    }
    const contactRows: Row[] = []
    for (const c of contacts) {
      if (c.is_hidden) continue
      const name = c.name || c.business_name || c.push_name || ('+' + (c.phone || ''))
      const subtitle = c.phone ? '+' + c.phone : ''
      if (needle && !haystackMatch(needle, name, subtitle)) continue
      contactRows.push({ kind: 'contact', jid: c.jid, title: name, subtitle })
    }
    contactRows.sort((a, b) => a.title.localeCompare(b.title))
    const groupRows: Row[] = []
    for (const g of groups) {
      if (needle && !haystackMatch(needle, g.name, '')) continue
      groupRows.push({ kind: 'group', jid: g.jid, title: g.name || 'Group', subtitle: 'Group' })
    }
    groupRows.sort((a, b) => a.title.localeCompare(b.title))
    // Cap to keep the modal snappy even with thousands of contacts.
    return [...out, ...contactRows, ...groupRows].slice(0, 200)
  }, [contacts, groups, q, selfDevice?.jid])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-[460px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">New chat</h2>
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
          {matches.length === 0 ? (
            <div className="py-10 text-center text-xs text-neutral-600">
              {q.trim() ? 'No matches.' : 'No contacts yet.'}
            </div>
          ) : (
            matches.map((r) => (
              <button
                key={r.kind + ':' + r.jid}
                onClick={() => {
                  onPick(r.jid)
                  onClose()
                }}
                className="flex w-full items-center gap-3 border-b border-neutral-900 px-4 py-2.5 text-left transition hover:bg-neutral-800/60"
              >
                <ChatAvatar
                  jid={r.jid}
                  title={r.title}
                  group={r.kind === 'group' || isGroup(r.jid)}
                  size={36}
                />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm text-neutral-100">
                    {r.title}
                  </div>
                  {r.subtitle && (
                    <div className="truncate text-[11px] text-neutral-500">{r.subtitle}</div>
                  )}
                </div>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

function haystackMatch(needle: string, ...fields: string[]): boolean {
  for (const f of fields) {
    if (f && f.toLowerCase().includes(needle)) return true
  }
  return false
}

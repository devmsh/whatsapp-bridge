import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Contact } from '../api'
import { ChatAvatar } from './ChatAvatar'

// AddMembersModal — WA's "Add members" sheet. Opens from the "+ Add
// participants" row in Group info. Search across all contacts, multi-select,
// then submit → bridge POSTs every selected JID with action='add' in one
// batched call. Owner/admin gating happens upstream in whatsmeow; we surface
// the failure inline and let the user retry / cancel.
//
// Excludes contacts who are already in the group (we don't want to send
// "add" for someone already a member) and hidden contacts (private vault).
export function AddMembersModal({
  groupJID,
  existingJIDs,
  onClose,
  onAdded,
}: {
  groupJID: string
  /** JIDs already in the group — filtered out of the picker. Pass both
   *  jid and lid forms for each member so dual-identity contacts don't
   *  show up as "addable". */
  existingJIDs: Set<string>
  onClose: () => void
  /** Called once the bridge confirms the add succeeded — parent should
   *  refetch its participant list. */
  onAdded: () => void
}) {
  const [contacts, setContacts] = useState<Contact[] | null>(null)
  const [q, setQ] = useState('')
  const [picked, setPicked] = useState<Set<string>>(() => new Set())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api
      .contacts()
      .then(setContacts)
      .catch(() => setError('Failed to load contacts.'))
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

  const candidates = useMemo(() => {
    if (!contacts) return []
    const needle = q.trim().toLowerCase()
    const out: Contact[] = []
    for (const c of contacts) {
      if (c.is_hidden) continue
      if (existingJIDs.has(c.jid)) continue
      if (c.lid && existingJIDs.has(c.lid)) continue
      const name = c.name || c.verified_name || c.business_name || c.push_name || ''
      const phone = c.phone || ''
      if (
        needle &&
        !name.toLowerCase().includes(needle) &&
        !phone.includes(needle)
      ) {
        continue
      }
      out.push(c)
    }
    // Same sort the NewChatModal uses — contacts ordered alphabetically.
    out.sort((a, b) =>
      (a.name || a.verified_name || a.business_name || a.phone || '').localeCompare(
        b.name || b.verified_name || b.business_name || b.phone || '',
      ),
    )
    // Cap at 300 to keep the list snappy even with thousands of contacts.
    return out.slice(0, 300)
  }, [contacts, q, existingJIDs])

  function toggle(jid: string) {
    setPicked((prev) => {
      const next = new Set(prev)
      if (next.has(jid)) next.delete(jid)
      else next.add(jid)
      return next
    })
  }

  async function submit() {
    if (picked.size === 0 || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await api.groupParticipantsUpdate(groupJID, Array.from(picked), 'add')
      onAdded()
      onClose()
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message + ' — only group admins can add members.'
          : 'Could not add. Only group admins can add members.',
      )
    } finally {
      setSubmitting(false)
    }
  }

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
          <h2 className="text-sm font-semibold text-neutral-100">
            Add participants
            {picked.size > 0 && (
              <span className="ml-2 rounded-full bg-emerald-500/20 px-2 py-0.5 text-[11px] font-medium text-emerald-200">
                {picked.size}
              </span>
            )}
          </h2>
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
          {contacts !== null && candidates.length === 0 && (
            <div className="py-10 text-center text-xs text-neutral-600">
              {q.trim() ? 'No matches.' : 'Everyone already in this group.'}
            </div>
          )}
          {candidates.map((c) => {
            const name = c.name || c.business_name || c.push_name || ('+' + (c.phone || ''))
            const sub = c.phone ? '+' + c.phone : ''
            const isPicked = picked.has(c.jid)
            return (
              <button
                key={c.jid}
                onClick={() => toggle(c.jid)}
                className={
                  'flex w-full items-center gap-3 border-b border-neutral-900 px-4 py-2.5 text-left transition ' +
                  (isPicked ? 'bg-emerald-500/10' : 'hover:bg-neutral-800/60')
                }
              >
                <ChatAvatar jid={c.jid} title={name} size={36} />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm text-neutral-100">
                    {name}
                  </div>
                  {sub && (
                    <div className="truncate text-[11px] text-neutral-500">{sub}</div>
                  )}
                </div>
                <input
                  type="checkbox"
                  checked={isPicked}
                  readOnly
                  className="pointer-events-none h-4 w-4 accent-emerald-500"
                />
              </button>
            )
          })}
        </div>

        {error && (
          <div className="border-t border-red-900/60 bg-red-950/40 px-4 py-2 text-[11px] text-red-300">
            {error}
          </div>
        )}

        <footer className="flex items-center justify-end gap-2 border-t border-neutral-800 px-4 py-3">
          <button
            onClick={onClose}
            disabled={submitting}
            className="rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-200 transition hover:bg-neutral-800 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={submit}
            disabled={submitting || picked.size === 0}
            className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
          >
            {submitting
              ? 'Adding…'
              : picked.size === 0
                ? 'Pick someone'
                : picked.size === 1
                  ? 'Add 1 member'
                  : `Add ${picked.size} members`}
          </button>
        </footer>
      </div>
    </div>
  )
}

import { useEffect, useState } from 'react'
import { api, type GroupJoinRequest } from '../api'
import { ChatAvatar } from './ChatAvatar'

// GroupRequestsSection lists pending join requests for one group. Lives
// inside GroupInfoModal, above the participant list. Admin-only on WA's
// side; non-admins get an empty list from the bridge so the section never
// appears for them.
//
// Each row shows the requester's avatar + name (or +phone) + how long ago
// the request landed, plus two buttons: ✓ Approve / ✕ Reject. Either
// flips the local state (drops the row) and POSTs the batched action to
// the bridge; failures revert the row and surface inline.
export function GroupRequestsSection({
  jid,
  nameMap,
}: {
  jid: string
  nameMap: Map<string, string>
}) {
  const [requests, setRequests] = useState<GroupJoinRequest[] | null>(null)
  const [busyJID, setBusyJID] = useState<string | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api
      .groupRequests(jid)
      .then((r) => {
        if (!cancelled) setRequests(r)
      })
      .catch(() => {
        if (!cancelled) setRequests([]) // silent: non-admin or transient
      })
    return () => {
      cancelled = true
    }
  }, [jid])

  async function decide(j: string, action: 'approve' | 'reject') {
    if (busyJID) return
    setBusyJID(j)
    setError('')
    // Optimistic drop — flip the row out immediately so the user feels the
    // action land. Failure restores it from the original list.
    const prev = requests
    setRequests((r) => (r || []).filter((x) => x.JID !== j))
    try {
      await api.groupRequestsUpdate(jid, [j], action)
    } catch (e) {
      setRequests(prev)
      setError(
        e instanceof Error
          ? e.message + ' — only group admins can decide requests.'
          : 'Action failed.',
      )
    } finally {
      setBusyJID(null)
    }
  }

  // Hidden when no pending requests — keeps Group info compact for non-admin
  // and quiet-group cases.
  if (!requests || requests.length === 0) return null

  return (
    <section className="border-b border-neutral-800 px-4 py-3">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
          Pending requests
        </span>
        <span className="rounded-full bg-amber-500/20 px-2 py-0.5 text-[10px] font-medium text-amber-200">
          {requests.length}
        </span>
      </div>
      <ul className="flex flex-col gap-1.5">
        {requests.map((r) => {
          const name =
            nameMap.get(r.JID) ||
            '+' + (r.JID.split('@')[0] || '').split(':')[0]
          const ago = humanAgo(r.RequestedAt)
          const isBusy = busyJID === r.JID
          return (
            <li
              key={r.JID}
              className="flex items-center gap-3 rounded-md bg-neutral-900/60 px-2 py-1.5"
            >
              <ChatAvatar jid={r.JID} title={name} size={32} />
              <div className="min-w-0 flex-1">
                <div dir="auto" className="truncate text-sm text-neutral-100">
                  {name}
                </div>
                <div className="text-[11px] text-neutral-500">requested {ago}</div>
              </div>
              <button
                onClick={() => void decide(r.JID, 'reject')}
                disabled={isBusy}
                title="Decline request"
                aria-label="Decline request"
                className="flex h-7 w-7 items-center justify-center rounded-full text-neutral-400 transition hover:bg-red-500/20 hover:text-red-300 disabled:opacity-50"
              >
                ✕
              </button>
              <button
                onClick={() => void decide(r.JID, 'approve')}
                disabled={isBusy}
                title="Approve request"
                aria-label="Approve request"
                className="flex h-7 w-7 items-center justify-center rounded-full bg-emerald-600 text-white transition hover:bg-emerald-500 disabled:opacity-50"
              >
                ✓
              </button>
            </li>
          )
        })}
      </ul>
      {error && (
        <div className="mt-2 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-[11px] text-red-300">
          {error}
        </div>
      )}
    </section>
  )
}

// humanAgo: short relative-time string for join-request rows.
// "just now" → "5m ago" → "3h ago" → "2d ago" → ISO-ish date fallback.
function humanAgo(iso: string): string {
  const t = Date.parse(iso)
  if (!Number.isFinite(t)) return ''
  const diff = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  if (diff < 86400 * 14) return `${Math.floor(diff / 86400)}d ago`
  return new Date(t).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

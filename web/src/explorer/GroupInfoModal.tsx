import { useEffect, useState } from 'react'
import { api, type GroupParticipant } from '../api'
import { ChatAvatar } from './ChatAvatar'

// GroupInfoModal mirrors WA's "Group info" — a focused panel showing the
// group's avatar, name, member count, and the participant list with
// admin / owner badges. Click a member row to jump straight to a DM
// with them (the modal closes), exactly like the official client.
//
// This is intentionally narrower than the existing Dashboard: no
// tasks, no AI profile, no charts — just who's in the group and how
// to reach them. The Dashboard remains reachable from the chat
// header avatar; this is the lightweight peek.
export function GroupInfoModal({
  jid,
  title,
  memberCount,
  nameMap,
  onClose,
  onOpenChat,
}: {
  jid: string
  title: string
  /** Pre-loaded count from MessageThread's group-header subtitle, used
   *  as a placeholder while the full participant list is fetching. */
  memberCount: number | null
  nameMap: Map<string, string>
  onClose: () => void
  onOpenChat: (jid: string) => void
}) {
  const [participants, setParticipants] = useState<GroupParticipant[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api.groupParticipants(jid).then((p) => {
      if (!cancelled) setParticipants(p)
    }).catch((e) => {
      if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load members')
    })
    return () => { cancelled = true }
  }, [jid])

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

  // Sort: owner first, then admins, then everyone else alphabetically.
  // Matches WA's own participant list.
  const sorted = (participants || [])
    .slice()
    .sort((a, b) => {
      const aRank = a.is_super_admin ? 2 : a.is_admin ? 1 : 0
      const bRank = b.is_super_admin ? 2 : b.is_admin ? 1 : 0
      if (aRank !== bRank) return bRank - aRank
      const an = nameOf(a, nameMap).toLowerCase()
      const bn = nameOf(b, nameMap).toLowerCase()
      return an.localeCompare(bn)
    })

  const count = participants?.length ?? memberCount ?? 0

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[460px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Group info</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        {/* Hero: big group avatar + name + member count. Avatar is
            clickable to open the full-screen profile photo preview
            (cycle 12), so tapping it gives the WA "tap to see picture"
            gesture without leaking it elsewhere. */}
        <div className="flex flex-col items-center gap-2 border-b border-neutral-800 px-4 py-5">
          <ChatAvatar jid={jid} title={title} group size={96} clickable />
          <div dir="auto" className="text-base font-semibold text-neutral-100">
            {title}
          </div>
          <div className="text-[11px] uppercase tracking-wider text-neutral-500">
            {count} {count === 1 ? 'member' : 'members'}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto py-1">
          {error && (
            <div className="p-4 text-center text-xs text-red-400">{error}</div>
          )}
          {!error && participants === null && (
            <div className="p-6 text-center text-xs text-neutral-600">Loading members…</div>
          )}
          {sorted.map((p) => {
            const name = nameOf(p, nameMap)
            const phone = p.phone ? '+' + p.phone : ''
            return (
              <button
                key={p.jid}
                onClick={() => {
                  onOpenChat(p.jid)
                  onClose()
                }}
                className="flex w-full items-center gap-3 px-4 py-2 text-left transition hover:bg-neutral-800/60"
              >
                <ChatAvatar jid={p.jid} title={name} size={36} />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm text-neutral-100">
                    {name}
                  </div>
                  {phone && phone !== name && (
                    <div className="truncate text-[11px] text-neutral-500">{phone}</div>
                  )}
                </div>
                {(p.is_admin || p.is_super_admin) && (
                  <span className="shrink-0 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] uppercase tracking-wider text-emerald-300">
                    {p.is_super_admin ? 'Owner' : 'Admin'}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function nameOf(p: GroupParticipant, nameMap: Map<string, string>): string {
  return (
    p.display_name ||
    nameMap.get(p.jid) ||
    (p.phone ? '+' + p.phone : '') ||
    '+' + (p.jid.split('@')[0] || '').replace(/:.*/, '')
  )
}

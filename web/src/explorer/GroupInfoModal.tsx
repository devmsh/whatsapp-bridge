import { useEffect, useState } from 'react'
import { api, type GroupParticipant } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { DisappearingSection } from './DisappearingSection'
import { GroupAdminSection } from './GroupAdminSection'

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

        <InviteLinkSection jid={jid} title={title} />
        <DisappearingSection jid={jid} isGroup={true} />
        <GroupAdminSection jid={jid} />

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

// InviteLinkSection — the "Invite to group via link" row WA shows at the top
// of any group info page. Collapsed by default so the modal stays compact;
// expanding fetches the current link from the bridge (admin-only on WA's
// side, so we surface a friendly error for non-admin groups instead of a
// raw 500). Expanded view offers Copy, native Share (mobile / browsers that
// support it), and a Reset action that mints a fresh code and revokes the
// old one — gated behind a confirm because every existing link breaks.
function InviteLinkSection({ jid, title }: { jid: string; title: string }) {
  const [open, setOpen] = useState(false)
  const [link, setLink] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  // Transient one-liner under the buttons — "Copied!", "Link reset.",
  // cleared after a short timeout so the panel doesn't stay in a stale
  // success state.
  const [hint, setHint] = useState('')

  function load(reset = false) {
    setLoading(true)
    setError('')
    api
      .groupInviteLink(jid, reset)
      .then((l) => {
        setLink(l)
        if (reset) flashHint('Link reset — old link no longer works.')
      })
      .catch(() => setError("Couldn't load the invite link. Only group admins can manage it."))
      .finally(() => setLoading(false))
  }

  function flashHint(s: string) {
    setHint(s)
    window.setTimeout(() => setHint(''), 2400)
  }

  // First open lazily fetches. Closing keeps the cached link so reopening
  // is instant — only Reset triggers another bridge round-trip.
  function toggle() {
    const next = !open
    setOpen(next)
    if (next && !link && !loading && !error) load(false)
  }

  async function copy() {
    if (!link) return
    try {
      await navigator.clipboard.writeText(link)
      flashHint('Copied!')
    } catch {
      flashHint('Copy failed — select the link manually.')
    }
  }

  async function share() {
    if (!link) return
    const data: ShareData = {
      title,
      text: `Join the WhatsApp group "${title}"`,
      url: link,
    }
    // navigator.share is only available on mobile + a handful of
    // browsers; the button is hidden upstream when missing.
    try {
      await navigator.share(data)
    } catch {
      /* user cancelled — silent */
    }
  }

  function reset() {
    if (
      !window.confirm(
        'Reset the invite link?\n\nThe current link will stop working immediately. Anyone you shared it with will need the new one.',
      )
    ) return
    load(true)
  }

  // Whether the browser's native share sheet is available (mobile +
  // Edge / Safari desktop). When absent we just hide the button.
  const canShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function'

  return (
    <section className="border-b border-neutral-800">
      <button
        onClick={toggle}
        className="flex w-full items-center gap-3 px-4 py-3 text-left transition hover:bg-neutral-800/40"
      >
        <span className="flex h-9 w-9 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-300">
          {/* Chain-link icon — same glyph WA uses for "Invite via link". */}
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.5 1.5" />
            <path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.5-1.5" />
          </svg>
        </span>
        <span className="flex-1 text-sm text-neutral-100">Invite to group via link</span>
        <span className="text-xs text-neutral-500">{open ? '−' : '+'}</span>
      </button>

      {open && (
        <div className="px-4 pb-4">
          {loading && !link && (
            <div className="py-2 text-xs text-neutral-500">Loading link…</div>
          )}
          {error && (
            <div className="rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}
          {link && (
            <>
              <div className="mb-2 break-all rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-2 font-mono text-[12px] text-emerald-300">
                {link}
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={copy}
                  className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-emerald-500"
                >
                  Copy link
                </button>
                {canShare && (
                  <button
                    onClick={share}
                    className="rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-200 transition hover:bg-neutral-800"
                  >
                    Share…
                  </button>
                )}
                <button
                  onClick={reset}
                  disabled={loading}
                  className="rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-200 transition hover:bg-neutral-800 disabled:opacity-50"
                  title="Revoke the current link and mint a new one"
                >
                  Reset link
                </button>
              </div>
              {hint && <div className="mt-2 text-xs text-emerald-400">{hint}</div>}
            </>
          )}
        </div>
      )}
    </section>
  )
}

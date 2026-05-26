import { useEffect, useState } from 'react'
import { api, type GroupParticipant } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { DisappearingSection } from './DisappearingSection'
import { GroupAdminSection } from './GroupAdminSection'
import { AddMembersModal } from './AddMembersModal'

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
  // When true, the AddMembersModal is open over this one. Closes via Esc,
  // outside-click, or after a successful add (which also triggers a
  // participant-list refresh).
  const [adding, setAdding] = useState(false)

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
            (cycle 12). Name doubles as an inline rename (click the
            pencil → input → Save). Description row sits below the
            count, also inline-editable. Both edits go straight to the
            bridge; non-admin attempts surface a friendly error. */}
        <div className="flex flex-col items-center gap-2 border-b border-neutral-800 px-4 py-5">
          <ChatAvatar jid={jid} title={title} group size={96} clickable />
          <EditableGroupName jid={jid} initial={title} />
          <div className="text-[11px] uppercase tracking-wider text-neutral-500">
            {count} {count === 1 ? 'member' : 'members'}
          </div>
          <EditableGroupDescription jid={jid} />
        </div>

        <InviteLinkSection jid={jid} title={title} />
        <DisappearingSection jid={jid} isGroup={true} />
        <GroupAdminSection jid={jid} />

        <div className="flex-1 overflow-y-auto py-1">
          {/* "+ Add participants" row — admin-only on the server (whatsmeow
              rejects non-admin "add" calls). We don't gate on the client
              yet (selfDevice isn't threaded in), so anyone can tap and
              the modal will show the error inline if it bounces. */}
          <button
            onClick={() => setAdding(true)}
            className="flex w-full items-center gap-3 px-4 py-2 text-left transition hover:bg-neutral-800/60"
          >
            <span className="flex h-9 w-9 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-300">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M19 8v6" />
                <path d="M22 11h-6" />
              </svg>
            </span>
            <span className="text-sm font-medium text-emerald-300">Add participants</span>
          </button>

          {error && (
            <div className="p-4 text-center text-xs text-red-400">{error}</div>
          )}
          {!error && participants === null && (
            <div className="p-6 text-center text-xs text-neutral-600">Loading members…</div>
          )}
          {sorted.map((p) => (
            <ParticipantRow
              key={p.jid}
              groupJID={jid}
              participant={p}
              displayName={nameOf(p, nameMap)}
              onOpenChat={(j) => {
                onOpenChat(j)
                onClose()
              }}
              onChanged={() => {
                // Refetch the member list — promote/demote flips the badge,
                // remove drops the row entirely. Cheaper than maintaining a
                // local override map.
                api.groupParticipants(jid).then(setParticipants).catch(() => {})
              }}
            />
          ))}
        </div>
      </div>

      {adding && (
        <AddMembersModal
          groupJID={jid}
          existingJIDs={
            // Build the exclusion set from both .jid AND .lid forms so a
            // dual-identity contact isn't offered as "addable" just because
            // their phone JID differs from their lid.
            new Set(
              (participants || []).flatMap((p) =>
                p.lid ? [p.jid, p.lid] : [p.jid],
              ),
            )
          }
          onClose={() => setAdding(false)}
          onAdded={() => {
            // Refetch so the new members show in the list with correct
            // sort order and admin badges. The AddMembersModal also closes
            // itself on success.
            api.groupParticipants(jid).then(setParticipants).catch(() => {})
          }}
        />
      )}
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

// ParticipantRow renders one member of the group with the WA-style admin
// menu — kebab on the right opens Promote / Demote / Remove, action runs
// against the bridge, parent refetches the list on success.
//
// Visibility rules mirror WA:
//   - Owner (is_super_admin): no menu — the owner can't be touched.
//   - Admin: menu shows Demote + Remove.
//   - Plain member: menu shows Promote + Remove.
//
// The whole row is also clickable (avatar + name area) — opens a DM with
// that participant, exactly the cycle-38 gesture. We don't nest a button
// inside a button (invalid HTML) — left half is a button, right half is a
// kebab button + admin pill in a flex row.
function ParticipantRow({
  groupJID,
  participant: p,
  displayName,
  onOpenChat,
  onChanged,
}: {
  groupJID: string
  participant: GroupParticipant
  displayName: string
  onOpenChat: (jid: string) => void
  onChanged: () => void
}) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const phone = p.phone ? '+' + p.phone : ''

  async function run(action: 'promote' | 'demote' | 'remove') {
    if (busy) return
    if (action === 'remove') {
      const ok = window.confirm(
        `Remove ${displayName} from this group?\n\nThey'll stop seeing new messages immediately. You can add them back later if you have their number.`,
      )
      if (!ok) return
    }
    setBusy(true)
    setOpen(false)
    try {
      await api.groupParticipantsUpdate(groupJID, [p.jid], action)
      onChanged()
    } catch (e) {
      window.alert(
        `Couldn't ${action} ${displayName}: ${e instanceof Error ? e.message : 'unknown error'}\n\nOnly group admins can manage members.`,
      )
    } finally {
      setBusy(false)
    }
  }

  // Owner is untouchable.
  const showMenu = !p.is_super_admin

  return (
    <div className="group relative flex w-full items-center gap-3 px-4 py-2 transition hover:bg-neutral-800/60">
      <button
        onClick={() => onOpenChat(p.jid)}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
        title="Open a DM with this contact"
      >
        <ChatAvatar jid={p.jid} title={displayName} size={36} />
        <div className="min-w-0 flex-1">
          <div dir="auto" className="truncate text-sm text-neutral-100">
            {displayName}
          </div>
          {phone && phone !== displayName && (
            <div className="truncate text-[11px] text-neutral-500">{phone}</div>
          )}
        </div>
      </button>
      {(p.is_admin || p.is_super_admin) && (
        <span className="shrink-0 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] uppercase tracking-wider text-emerald-300">
          {p.is_super_admin ? 'Owner' : 'Admin'}
        </span>
      )}
      {showMenu && (
        <div className="relative">
          <button
            onClick={() => setOpen((v) => !v)}
            disabled={busy}
            title="Manage member"
            aria-label="Manage member"
            className={
              'flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-700 hover:text-neutral-200 disabled:opacity-50 ' +
              // Always visible when busy or menu open; otherwise reveal on
              // row hover. Same affordance pattern as the bubble action
              // cluster — silent in the background, easy to find.
              (busy || open ? 'opacity-100' : 'opacity-0 group-hover:opacity-100')
            }
          >
            {/* Vertical 3-dot kebab */}
            <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true">
              <circle cx="12" cy="5" r="1.5" />
              <circle cx="12" cy="12" r="1.5" />
              <circle cx="12" cy="19" r="1.5" />
            </svg>
          </button>
          {open && (
            <>
              {/* Click-away catcher: stops the menu staying open when the
                  user clicks somewhere else in the modal. */}
              <div
                onClick={() => setOpen(false)}
                className="fixed inset-0 z-30"
                aria-hidden="true"
              />
              <div className="absolute right-0 top-full z-40 mt-1 w-44 overflow-hidden rounded-lg border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60">
                {p.is_admin ? (
                  <MenuItem onClick={() => run('demote')}>Dismiss as admin</MenuItem>
                ) : (
                  <MenuItem onClick={() => run('promote')}>Make group admin</MenuItem>
                )}
                <MenuItem destructive onClick={() => run('remove')}>
                  Remove from group
                </MenuItem>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

// EditableGroupName renders the title in display mode with a tiny pencil
// affordance on hover. Clicking the pencil swaps to a single-line input;
// Enter / blur saves, Esc cancels. WA mobile flows the same way — name is
// not always editable to non-admins, so we let the bridge gate it and
// surface the error inline if the save bounces.
function EditableGroupName({ jid, initial }: { jid: string; initial: string }) {
  const [value, setValue] = useState(initial)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(initial)
  const [error, setError] = useState('')
  // Keep our display value in sync if the parent's `initial` changes
  // (e.g. chats refetch after we save it ourselves).
  useEffect(() => {
    setValue(initial)
    setDraft(initial)
  }, [initial])

  async function save() {
    const next = draft.trim()
    if (!next || next === value) {
      setEditing(false)
      setDraft(value)
      return
    }
    setError('')
    try {
      await api.groupRename(jid, next)
      setValue(next)
      setEditing(false)
      // Chats prop in the parent reads this name; refetch the list so the
      // chat header and chat-list row pick up the new title.
      window.dispatchEvent(new CustomEvent('wa.chats-changed', { detail: { jid } }))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed — only group admins can rename.')
    }
  }

  if (editing) {
    return (
      <div className="flex w-full max-w-xs flex-col items-stretch gap-1">
        <input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={save}
          onKeyDown={(e) => {
            if (e.key === 'Enter') save()
            else if (e.key === 'Escape') {
              setDraft(value)
              setEditing(false)
              setError('')
            }
          }}
          maxLength={100}
          className="w-full rounded-md border border-neutral-700 bg-neutral-950 px-3 py-1.5 text-center text-base font-semibold text-neutral-100 outline-none focus:border-emerald-500"
        />
        {error && (
          <div className="text-center text-[11px] text-red-300">{error}</div>
        )}
      </div>
    )
  }

  return (
    <button
      onClick={() => setEditing(true)}
      title="Rename group (admin)"
      className="group/n flex items-center gap-1.5 text-base font-semibold text-neutral-100 transition hover:text-emerald-200"
    >
      <span dir="auto">{value}</span>
      <svg
        viewBox="0 0 24 24"
        width="13"
        height="13"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="opacity-0 transition group-hover/n:opacity-70"
        aria-hidden="true"
      >
        <path d="M12 20h9" />
        <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4z" />
      </svg>
    </button>
  )
}

// EditableGroupDescription is the bigger sibling — multi-line textarea on
// edit, single placeholder ("Add a description") when empty. Fetches the
// current description on mount via api.groupGet so we don't have to thread
// it down from the parent. Same admin-gating + inline error pattern as the
// name editor.
function EditableGroupDescription({ jid }: { jid: string }) {
  const [value, setValue] = useState<string | null>(null) // null while loading
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api
      .groupGet(jid)
      .then((g) => {
        if (cancelled) return
        const t = g.topic || ''
        setValue(t)
        setDraft(t)
      })
      .catch(() => {
        if (!cancelled) setValue('')
      })
    return () => {
      cancelled = true
    }
  }, [jid])

  async function save() {
    const next = draft.trim()
    if (next === (value || '').trim()) {
      setEditing(false)
      return
    }
    setSaving(true)
    setError('')
    try {
      await api.groupSetDescription(jid, next)
      setValue(next)
      setEditing(false)
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : 'Save failed — only group admins can change this.',
      )
    } finally {
      setSaving(false)
    }
  }

  if (value === null) return null // loading

  if (editing) {
    return (
      <div className="mt-1 flex w-full flex-col items-stretch gap-1">
        <textarea
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              setDraft(value || '')
              setEditing(false)
              setError('')
            }
          }}
          maxLength={512}
          rows={3}
          placeholder="Add a description"
          className="w-full resize-none rounded-md border border-neutral-700 bg-neutral-950 px-3 py-2 text-xs text-neutral-200 outline-none focus:border-emerald-500"
        />
        <div className="flex justify-end gap-2">
          <button
            onClick={() => {
              setDraft(value || '')
              setEditing(false)
              setError('')
            }}
            disabled={saving}
            className="rounded-md border border-neutral-700 px-2.5 py-1 text-[11px] text-neutral-300 transition hover:bg-neutral-800 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={save}
            disabled={saving}
            className="rounded-md bg-emerald-600 px-2.5 py-1 text-[11px] font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
        {error && <div className="text-[11px] text-red-300">{error}</div>}
      </div>
    )
  }

  return (
    <button
      onClick={() => setEditing(true)}
      title="Edit group description (admin)"
      className="mt-1 max-w-full whitespace-pre-wrap break-words rounded-md px-3 py-1 text-center text-[12px] text-neutral-400 transition hover:bg-neutral-800/40 hover:text-neutral-200"
    >
      {value ? value : <span className="italic text-neutral-600">Add a description</span>}
    </button>
  )
}

function MenuItem({
  children,
  destructive = false,
  onClick,
}: {
  children: React.ReactNode
  destructive?: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={
        'block w-full px-3 py-2 text-left text-xs transition hover:bg-neutral-800 ' +
        (destructive ? 'text-red-300' : 'text-neutral-200')
      }
    >
      {children}
    </button>
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

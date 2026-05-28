import { useEffect, useMemo, useState } from 'react'
import { api, type Chat, type Circle } from '../api'
import { chatListTime, chatTitle, isGroup, previewText } from './format'
import { ChatAvatar } from './ChatAvatar'
import { ContextMenu, type MenuItem } from './ContextMenu'
import { useDrafts } from '../hooks/useDrafts'
import { useChatWallpaper } from '../hooks/useChatWallpaper'
import { useChatLabels } from '../hooks/useChatLabels'
import { WallpaperPicker } from './WallpaperPicker'

// ChatList shows the time-ordered list of chats. Search is handled by the
// global SearchBar in the top bar — there's no per-list filter anymore.
// Right-click on any row opens a context menu with quick actions (Open, Hide,
// Unhide, Archive, Pin, Mark read/unread, Add/Remove from circle) so the user
// can act without first opening the chat.
export function ChatList({
  chats,
  nameMap,
  circles,
  selected,
  onOpen,
  onRequestHide,
  onChanged,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  circles: Circle[]
  selected: string | null
  onOpen: (jid: string) => void
  onRequestHide: (jid: string, title: string) => void
  onChanged: () => void
}) {
  // Live snapshot of per-chat drafts — the Composer dispatches
  // `wa.draft-changed` on every keystroke / send so the "Draft: …" pill on
  // matching rows updates immediately as the user types or sends. Map<jid, text>.
  const drafts = useDrafts()
  // Map<chatJID, senderJID[]> of every chat the bridge currently sees a fresh
  // 'composing' beacon for. Drives the WA-style "typing…" preview that
  // replaces the last-message line. One poll per tick covers every visible
  // row (groups + DMs combined) — see /api/v2/typing on the bridge.
  const typing = useTypingSnapshot()
  // WA Business labels → colored dots on each row. Read once here (hook can't
  // run inside the row map) and resolve ids to {name,color} per row.
  const { labels: chatLabels, assignments: labelAssignments } = useChatLabels()
  const labelById = useMemo(
    () => new Map(chatLabels.map((l) => [l.id, l] as const)),
    [chatLabels],
  )
  // Filter the list to one label (WA Business "tap a label to see its chats").
  const [labelFilter, setLabelFilter] = useState<string | null>(null)
  // Only labels actually in use get a filter chip — no point offering a
  // filter that would always be empty.
  const usedLabelIds = useMemo(() => {
    const s = new Set<string>()
    for (const ids of Object.values(labelAssignments)) for (const id of ids) s.add(id)
    return s
  }, [labelAssignments])
  // Clear a stale filter if its label was deleted out from under us.
  useEffect(() => {
    if (labelFilter && !chatLabels.some((l) => l.id === labelFilter)) setLabelFilter(null)
  }, [labelFilter, chatLabels])
  const [menu, setMenu] = useState<{ jid: string; title: string; x: number; y: number } | null>(null)
  // When set, shows the circle-membership picker for this chat.
  const [picking, setPicking] = useState<{ jid: string; title: string } | null>(null)
  // When set, shows the mute-duration picker (8h / 1 week / Always).
  // WhatsApp asks the same three options every time you mute.
  const [muting, setMuting] = useState<{ jid: string; title: string } | null>(null)
  // When set, shows the wallpaper picker for this chat. Per-chat tints
  // are localStorage-only (browser-scoped) — the bridge never sees them.
  const [wallpapering, setWallpapering] = useState<{ jid: string; title: string } | null>(null)
  // 'normal' shows non-archived chats + an 'Archived (N)' header at the top
  // when there is anything archived; 'archived' shows the archived-only view
  // with a back affordance. Mirrors WhatsApp's Archived screen exactly.
  const [view, setView] = useState<'normal' | 'archived'>('normal')
  // WA's recent filter pills at the top of the chat list: All / Unread /
  // Groups / @Mentions. Each is a cheap predicate over the chat row — no
  // backend filter, no fetch — so toggling is instant. Only meaningful in
  // the normal view; archived stays unfiltered.
  type Filter = 'all' | 'unread' | 'groups' | 'mentions' | 'drafts'
  const [filter, setFilter] = useState<Filter>('all')

  // Split into archived + non-archived once so we don't re-filter on every
  // render, and the counter in the 'Archived (N)' header is cheap.
  // Within the normal bucket, pinned chats sort to the top (sub-sorted by
  // last_message_at), then unpinned by last_message_at — same ordering
  // WA's own chat list uses. The bridge's GET /chats doesn't promise a
  // pinned-first order, so we do it client-side.
  const { archivedRows, normalRows } = useMemo(() => {
    const archivedRows: { chat: Chat; title: string }[] = []
    const normalRows: { chat: Chat; title: string }[] = []
    for (const c of chats) {
      const row = { chat: c, title: chatTitle(c, nameMap) }
      // Hidden chats must never leak into the archived view, even if
      // their archived flag is also set — the hidden vault is its own
      // world and "Archived (N)" should stay anonymous-safe. Route them
      // to the normal list instead so private-mode users can still find
      // them (via the HiddenBadge); they're just never visible to a
      // bystander tapping "Archived".
      if (c.is_hidden) normalRows.push(row)
      else if (c.is_archived) archivedRows.push(row)
      else normalRows.push(row)
    }
    normalRows.sort((a, b) => {
      const ap = a.chat.is_pinned ? 1 : 0
      const bp = b.chat.is_pinned ? 1 : 0
      if (ap !== bp) return bp - ap
      return (b.chat.last_message_at || 0) - (a.chat.last_message_at || 0)
    })
    return { archivedRows, normalRows }
  }, [chats, nameMap])

  // Counts per filter on the normal list — drives both the filter pills'
  // optional badge and the rows that actually render. Computed once per
  // chats refresh so the pills don't flicker.
  const filterCounts = useMemo(() => {
    let unread = 0
    let groups = 0
    let mentions = 0
    let draftCount = 0
    for (const r of normalRows) {
      if ((r.chat.unread_count || 0) > 0) unread++
      if (isGroup(r.chat.jid)) groups++
      if ((r.chat.unread_mentions || 0) > 0) mentions++
      if (drafts.has(r.chat.jid)) draftCount++
    }
    return { unread, groups, mentions, drafts: draftCount }
  }, [normalRows, drafts])

  // Apply the active filter to normalRows. We deliberately don't touch
  // archivedRows — WA's archived view is its own world, always unfiltered.
  const filteredNormalRows = useMemo(() => {
    let rows = normalRows
    if (labelFilter) {
      rows = rows.filter((r) => (labelAssignments[r.chat.jid] || []).includes(labelFilter))
    }
    if (filter === 'all') return rows
    return rows.filter((r) => {
      if (filter === 'unread') return (r.chat.unread_count || 0) > 0
      if (filter === 'groups') return isGroup(r.chat.jid)
      if (filter === 'mentions') return (r.chat.unread_mentions || 0) > 0
      if (filter === 'drafts') return drafts.has(r.chat.jid)
      return true
    })
  }, [normalRows, filter, drafts, labelFilter, labelAssignments])

  const rows = view === 'archived' ? archivedRows : filteredNormalRows

  // If the user exhausts the archived view (e.g. unarchives the last one),
  // bounce back to the normal list so they're not stranded on an empty screen.
  useEffect(() => {
    if (view === 'archived' && archivedRows.length === 0) setView('normal')
  }, [view, archivedRows.length])

  // "Mark all read" handler — fires chatAction('read') for every chat in
  // the normal list with unread > 0, sequentially. WA exposes the same
  // bulk action via its chat-list overflow menu. We deliberately scope to
  // the current normal list (not archived, not hidden) so a stray
  // "Mark all" never silently touches a hidden chat. Optimistic refresh
  // via onChanged once at the end so the badge clears in one flash.
  async function markAllRead() {
    const targets = normalRows
      .map((r) => r.chat)
      .filter((c) => (c.unread_count || 0) > 0)
    if (targets.length === 0) return
    for (const c of targets) {
      try { await api.chatAction(c.jid, 'read') } catch {}
    }
    onChanged()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Filter pills — only in normal view (archived has its own affordance). */}
      {view === 'normal' && (
        <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-neutral-800 px-3 py-2">
          <FilterPill id="all" current={filter} onPick={setFilter}>All</FilterPill>
          <FilterPill id="unread" current={filter} onPick={setFilter} count={filterCounts.unread}>
            Unread
          </FilterPill>
          <FilterPill id="groups" current={filter} onPick={setFilter} count={filterCounts.groups}>
            Groups
          </FilterPill>
          {filterCounts.mentions > 0 && (
            <FilterPill id="mentions" current={filter} onPick={setFilter} count={filterCounts.mentions}>
              @ Mentions
            </FilterPill>
          )}
          {/* Drafts pill — only when there's at least one half-written
              message. Surfaces the "I started typing in some chat but
              never sent it" backlog so it doesn't get buried. Same red
              accent as the "Draft: …" per-row label uses. */}
          {filterCounts.drafts > 0 && (
            <FilterPill id="drafts" current={filter} onPick={setFilter} count={filterCounts.drafts}>
              Drafts
            </FilterPill>
          )}
          {/* Label filter chips — WA Business "tap a label to see its chats".
              Only labels in use appear; tapping toggles the single-label
              filter, tapping the active one clears it. */}
          {chatLabels
            .filter((l) => usedLabelIds.has(l.id))
            .map((l) => {
              const active = labelFilter === l.id
              return (
                <button
                  key={l.id}
                  onClick={() => setLabelFilter(active ? null : l.id)}
                  title={`Show chats labeled "${l.name}"`}
                  className={
                    'flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium transition ' +
                    (active
                      ? 'text-neutral-50'
                      : 'bg-neutral-800/70 text-neutral-300 hover:bg-neutral-700/70')
                  }
                  style={
                    active
                      ? { backgroundColor: l.color + '33', boxShadow: `inset 0 0 0 1px ${l.color}` }
                      : undefined
                  }
                >
                  <span
                    className="h-2 w-2 rounded-full"
                    style={{ backgroundColor: l.color }}
                    aria-hidden="true"
                  />
                  {l.name}
                </button>
              )
            })}
          {/* Mark-all-read sits at the right edge of the filter row and
              only appears when there's something to clear, so the row
              doesn't carry a dangling action in calm times. */}
          {filterCounts.unread > 0 && (
            <button
              onClick={markAllRead}
              title="Mark every chat as read"
              className="ml-auto shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium text-emerald-300 transition hover:bg-emerald-500/15"
            >
              ✓ Mark all read
            </button>
          )}
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {/* Archived-view header: takes the place of the first chat row, with
            a back arrow that returns to the normal list. Same height + paint
            as a row so the list doesn't shift when toggling. */}
        {view === 'archived' && (
          <button
            onClick={() => setView('normal')}
            className="flex w-full items-center gap-3 border-b border-neutral-800 px-3 py-3 text-left text-sm text-neutral-200 transition hover:bg-neutral-900"
          >
            <span aria-hidden="true" className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-neutral-800 text-base">
              ←
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-medium">Archived</div>
              <div className="text-xs text-neutral-500">{archivedRows.length} {archivedRows.length === 1 ? 'chat' : 'chats'}</div>
            </div>
          </button>
        )}

        {/* Normal-view 'Archived (N)' row: only shown when there's something
            to navigate to. Click → switches into archived view. */}
        {view === 'normal' && archivedRows.length > 0 && (
          <button
            onClick={() => setView('archived')}
            className="flex w-full items-center gap-3 border-b border-neutral-800 px-3 py-2.5 text-left transition hover:bg-neutral-900"
            title="View archived chats"
          >
            <span
              aria-hidden="true"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-neutral-800 text-neutral-400"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="21 8 21 21 3 21 3 8" />
                <rect x="1" y="3" width="22" height="5" />
                <line x1="10" y1="12" x2="14" y2="12" />
              </svg>
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-neutral-200">Archived</div>
            </div>
            <span className="shrink-0 text-xs text-neutral-500">
              {archivedRows.length}
            </span>
          </button>
        )}

        {rows.length === 0 && (
          <div className="p-6 text-center text-xs text-neutral-600">
            {view === 'archived'
              ? 'No archived chats'
              : filter === 'unread'
                ? 'All caught up 🎉'
                : filter === 'groups'
                  ? 'No groups'
                  : filter === 'mentions'
                    ? 'No unread mentions'
                    : filter === 'drafts'
                      ? 'No drafts'
                      : 'No chats'}
          </div>
        )}
        {rows.map(({ chat, title }) => (
          <button
            key={chat.jid}
            onClick={() => onOpen(chat.jid)}
            onContextMenu={(e) => {
              e.preventDefault()
              setMenu({ jid: chat.jid, title, x: e.clientX, y: e.clientY })
            }}
            className={
              'flex w-full items-center gap-3 px-3 py-2.5 text-left transition ' +
              (selected === chat.jid ? 'bg-neutral-800' : 'hover:bg-neutral-900')
            }
          >
            <ChatAvatar jid={chat.jid} title={title} group={isGroup(chat.jid)} size={40} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <span dir="auto" className="truncate text-sm font-medium">
                  {title}
                </span>
                <span className="flex shrink-0 items-center gap-1">
                  {(labelAssignments[chat.jid] || []).map((lid) => {
                    const l = labelById.get(lid)
                    if (!l) return null
                    return (
                      <span
                        key={lid}
                        title={l.name}
                        aria-label={`Label: ${l.name}`}
                        className="h-2 w-2 rounded-full"
                        style={{ backgroundColor: l.color }}
                      />
                    )
                  })}
                  <span className="text-[11px] text-neutral-500">
                    {chatListTime(chat.last_message_at)}
                  </span>
                </span>
              </div>
              <div dir="auto" className="flex items-center gap-1.5 truncate text-xs text-neutral-500">
                {drafts.has(chat.jid) ? (
                  // WA shows "Draft: <preview>" in red whenever the composer
                  // has typed-but-unsent text — replaces the last-message
                  // line entirely so the user spots unsent threads at a
                  // glance. We mirror that exactly: same row, red "Draft:"
                  // label, single-line preview of the saved buffer.
                  <span className="truncate">
                    <span className="font-medium text-red-400">Draft: </span>
                    <span className="text-neutral-400">{drafts.get(chat.jid)}</span>
                  </span>
                ) : typing.has(chat.jid) ? (
                  // Live typing beacon — peer (DM) or one+ participants
                  // (group) are composing right now. WA replaces the
                  // last-message line with this in emerald so it pops at a
                  // glance. Group rows name the typer when we have a single
                  // one, mirroring the header line — falls back to a generic
                  // "typing…" for DMs and multi-typer groups.
                  <span className="truncate font-medium text-emerald-400">
                    {typingPreview(chat.jid, typing.get(chat.jid) || [], nameMap)}
                  </span>
                ) : (
                  <span className="truncate">
                    {chat.last_message
                      ? previewText(chat.last_message, nameMap)
                      : isGroup(chat.jid)
                        ? 'Group'
                        : chat.jid.replace('@s.whatsapp.net', '')}
                  </span>
                )}
              </div>
            </div>
            <div className="ml-1 flex shrink-0 flex-col items-end gap-0.5">
              <div className="flex items-center gap-1 text-neutral-500">
                {/* Muted indicator: bell-with-slash sized to the time row.
                    Greys out the unread badge below so muted chats fade into
                    the background even when they have new messages — exactly
                    what official WA does. */}
                {chat.is_muted && (
                  <span title="Muted" aria-label="Muted">
                    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
                      <path d="M18.63 13A17.89 17.89 0 0 1 18 8" />
                      <path d="M6.26 6.26A5.86 5.86 0 0 0 6 8c0 7-3 9-3 9h14" />
                      <path d="M18 8a6 6 0 0 0-9.33-5" />
                      <line x1="1" y1="1" x2="23" y2="23" />
                    </svg>
                  </span>
                )}
                {/* Pinned indicator: angled pushpin. Always visible (no
                    hover), matching WA's persistent 📌 badge on pinned rows. */}
                {chat.is_pinned && (
                  <span title="Pinned" aria-label="Pinned" className="text-neutral-400">
                    <svg viewBox="0 0 24 24" width="12" height="12" fill="currentColor">
                      <path d="M16 4l4 4-5.5 1.5L11 13l3 7-2 1-4-6-4 4-1-1 4-4-6-4 1-2 7 3 3.5-3.5L14.5 1 16 4z" />
                    </svg>
                  </span>
                )}
              </div>
              <div className="flex items-center gap-1">
                {/* Mention chip: emerald @ pill, only when at least one of
                    the chat's unread messages mentions the current user.
                    Stays bright even on muted chats so a personal ping
                    doesn't get lost behind the de-saturated unread badge. */}
                {(chat.unread_mentions ?? 0) > 0 && (
                  <span
                    title={chat.unread_mentions === 1 ? 'You were mentioned' : `${chat.unread_mentions} mentions of you`}
                    className="shrink-0 rounded-full bg-emerald-500 px-1 text-[11px] font-semibold leading-tight text-neutral-950"
                  >
                    @
                  </span>
                )}
                {chat.unread_count > 0 && (
                  <span
                    className={
                      'shrink-0 rounded-full px-1.5 py-0.5 text-[11px] font-semibold ' +
                      (chat.is_muted
                        ? 'bg-neutral-700 text-neutral-300'
                        : 'bg-emerald-500 text-neutral-950')
                    }
                  >
                    {chat.unread_count}
                  </span>
                )}
              </div>
            </div>
          </button>
        ))}
      </div>
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          items={buildChatMenu(
            chats.find((c) => c.jid === menu.jid),
            menu.title,
            {
              onOpen,
              onRequestHide,
              onChanged,
              onRequestAddToCircle: (jid, title) => setPicking({ jid, title }),
              onRequestMute: (jid, title) => setMuting({ jid, title }),
              onRequestWallpaper: (jid, title) => setWallpapering({ jid, title }),
            },
          )}
          onClose={() => setMenu(null)}
        />
      )}
      {picking && (
        <CirclePickerModal
          jid={picking.jid}
          chatTitle={picking.title}
          circles={circles}
          onClose={() => setPicking(null)}
          onAdded={() => {
            setPicking(null)
            onChanged()
          }}
        />
      )}
      {muting && (
        <MuteDurationPicker
          jid={muting.jid}
          chatTitle={muting.title}
          onDone={() => {
            setMuting(null)
            onChanged()
          }}
          onClose={() => setMuting(null)}
        />
      )}
      {wallpapering && (
        <WallpaperPickerForChat
          jid={wallpapering.jid}
          title={wallpapering.title}
          onClose={() => setWallpapering(null)}
        />
      )}
    </div>
  )
}

// useTypingSnapshot polls /api/v2/typing every 3 s and returns a Map of
// chatJID -> senderJIDs. One request covers every visible chat row, so the
// list can render WA's "typing…" preview without N+1 polling. The bridge's
// caches age beacons out after ~10 s without a refresh, so the snapshot
// naturally stops mentioning a chat the moment the peer pauses.
//
// Polling stays at 3 s regardless of list size — typing is the kind of
// signal users notice fast, and the request is a single tiny in-memory
// + indexed-DB lookup on the bridge.
function useTypingSnapshot(): Map<string, string[]> {
  const [snap, setSnap] = useState<Map<string, string[]>>(() => new Map())
  useEffect(() => {
    let cancelled = false
    async function tick() {
      const obj = await api.typingSnapshot().catch(() => ({}) as Record<string, string[]>)
      if (cancelled) return
      // Only swap when the shape actually changed — avoids re-rendering
      // every row every 3 s when nobody's typing (the common case).
      setSnap((prev) => {
        const next = new Map(Object.entries(obj))
        if (mapEqual(prev, next)) return prev
        return next
      })
    }
    void tick()
    const h = setInterval(tick, 3000)
    return () => {
      cancelled = true
      clearInterval(h)
    }
  }, [])
  return snap
}

// mapEqual: shallow set-equality for the typing snapshot. Same JIDs typing
// in the same chats is treated as unchanged, even if the array order or
// reference differs — server returns map iteration order, which isn't
// stable across calls.
function mapEqual(a: Map<string, string[]>, b: Map<string, string[]>): boolean {
  if (a.size !== b.size) return false
  for (const [k, v] of a) {
    const w = b.get(k)
    if (!w) return false
    if (w.length !== v.length) return false
    // Both lists tend to be 1–3 entries, so sort + compare is fine.
    const av = [...v].sort()
    const bv = [...w].sort()
    for (let i = 0; i < av.length; i++) if (av[i] !== bv[i]) return false
  }
  return true
}

// typingPreview turns the chat-list row label for an active typing beacon.
// Mirrors the chat-header rules in a compressed form so the row stays
// scannable:
//
//   DM (group=false)        → "typing…"
//   Group, 1 typer          → "Sarah is typing…"
//   Group, 2+ typers        → "Sarah +1 is typing…" / "+N is typing…"
//
// nameMap resolves a JID to a display name; falls back to the bare phone
// when unknown so the row is never "@lid:1234" garbage.
function typingPreview(chatJID: string, typers: string[], nameMap: Map<string, string>): string {
  if (!isGroup(chatJID)) return 'typing…'
  if (typers.length === 0) return 'typing…'
  const first = nameMap.get(typers[0]) || ('+' + (typers[0].split('@')[0] || '').split(':')[0])
  const firstName = first.split(/\s+/)[0]
  if (typers.length === 1) return `${firstName} is typing…`
  return `${firstName} +${typers.length - 1} is typing…`
}

// WallpaperPickerForChat thinly wraps WallpaperPicker so the picker's
// state can come from the per-chat useChatWallpaper hook without the
// ChatList itself binding to every chat's wallpaper for every row.
function WallpaperPickerForChat({
  jid,
  title,
  onClose,
}: {
  jid: string
  title: string
  onClose: () => void
}) {
  const { color, setColor } = useChatWallpaper(jid)
  return (
    <WallpaperPicker
      active={color}
      title={title}
      onPick={setColor}
      onClose={onClose}
    />
  )
}

// FilterPill is one of the small chip buttons in the chat-list filter row.
// Active = emerald, others = neutral; an optional count badge mirrors WA's
// own "Unread · 3" / "Groups · 12" look.
function FilterPill<T extends string>({
  id,
  current,
  onPick,
  count,
  children,
}: {
  id: T
  current: T
  onPick: (id: T) => void
  count?: number
  children: React.ReactNode
}) {
  const active = current === id
  return (
    <button
      onClick={() => onPick(id)}
      className={
        'flex shrink-0 items-center gap-1 rounded-full px-3 py-1 text-xs font-medium transition ' +
        (active
          ? 'bg-emerald-500/20 text-emerald-200 ring-1 ring-emerald-500/40'
          : 'bg-neutral-800/70 text-neutral-300 hover:bg-neutral-700/70')
      }
    >
      <span>{children}</span>
      {count !== undefined && count > 0 && (
        <span
          className={
            'tabular-nums text-[10px] ' +
            (active ? 'text-emerald-300' : 'text-neutral-400')
          }
        >
          {count}
        </span>
      )}
    </button>
  )
}

// MuteDurationPicker is the small dialog WhatsApp pops when you choose Mute:
// the same three buckets — 8 hours, 1 week, Always. Backend's chatAction
// already accepts duration in hours (0 = forever), so this is purely a UI
// affordance over the existing endpoint.
function MuteDurationPicker({
  jid,
  chatTitle,
  onDone,
  onClose,
}: {
  jid: string
  chatTitle: string
  onDone: () => void
  onClose: () => void
}) {
  const [busy, setBusy] = useState<number | null>(null)

  // Esc closes — same dismissal contract as the other modals.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && busy === null) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  // duration is hours; 0 is the bridge's "forever" sentinel.
  async function pick(duration: number) {
    if (busy !== null) return
    setBusy(duration)
    try {
      await api.chatAction(jid, 'mute', duration)
      onDone()
    } catch {
      // Surface failure by just releasing the spinner — the row stays
      // unchanged, the user can retry.
      setBusy(null)
    }
  }

  const options: { label: string; hours: number }[] = [
    { label: '8 hours', hours: 8 },
    { label: '1 week', hours: 168 },
    { label: 'Always', hours: 0 },
  ]

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-sm overflow-hidden rounded-2xl bg-neutral-900 shadow-xl ring-1 ring-neutral-800"
      >
        <div className="border-b border-neutral-800 px-4 py-3">
          <div className="text-sm font-semibold text-neutral-100">Mute notifications</div>
          <div dir="auto" className="mt-0.5 truncate text-xs text-neutral-500">
            {chatTitle}
          </div>
        </div>
        <div className="flex flex-col py-1">
          {options.map((opt) => (
            <button
              key={opt.hours}
              onClick={() => pick(opt.hours)}
              disabled={busy !== null}
              className={
                'flex items-center justify-between px-4 py-2.5 text-left text-sm text-neutral-200 transition ' +
                (busy === opt.hours
                  ? 'bg-emerald-500/10'
                  : 'hover:bg-neutral-800 disabled:opacity-50')
              }
            >
              <span>{opt.label}</span>
              {busy === opt.hours && (
                <span className="text-xs text-emerald-300">…</span>
              )}
            </button>
          ))}
        </div>
        <div className="flex justify-end gap-2 border-t border-neutral-800 px-3 py-2">
          <button
            onClick={onClose}
            disabled={busy !== null}
            className="rounded-lg px-3 py-1.5 text-xs text-neutral-300 transition hover:bg-neutral-800 disabled:opacity-50"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}

// CirclePickerModal shows a tree of circles + sub-circles with a checkbox
// per row indicating whether the chat is a member. Clicking toggles
// membership (add or remove). The chat (group or DM contact) goes through the
// regular circle-members API.
function CirclePickerModal({
  jid,
  chatTitle,
  circles,
  onClose,
  onAdded,
}: {
  jid: string
  chatTitle: string
  circles: Circle[]
  onClose: () => void
  onAdded: () => void
}) {
  const [busy, setBusy] = useState<Set<number>>(new Set())
  const [error, setError] = useState<string | null>(null)
  // Locally-tracked member set so checkboxes flip instantly without a refetch.
  const [memberIDs, setMemberIDs] = useState<Set<number> | null>(null)
  const memberType: 'group' | 'contact' = jid.endsWith('@g.us') ? 'group' : 'contact'

  // Fetch which circles this chat is already in.
  useEffect(() => {
    api
      .circlesForMember(memberType, jid)
      .then((cs) => setMemberIDs(new Set((cs || []).map((c) => c.id))))
      .catch(() => setMemberIDs(new Set()))
  }, [jid, memberType])

  // Build the tree from parent_ids. A circle with no parent (or whose only
  // parents are not in this circles set) is a root. Each circle appears under
  // its FIRST listed parent so the user sees a clean tree even if a circle
  // happens to be a member of multiple parents.
  const tree = useMemo(() => buildCircleTree(circles), [circles])

  async function toggle(c: Circle) {
    if (busy.has(c.id) || !memberIDs) return
    setBusy((b) => new Set(b).add(c.id))
    setError(null)
    const isMember = memberIDs.has(c.id)
    try {
      if (isMember) {
        await api.removeCircleMember(c.id, memberType, jid)
        setMemberIDs((s) => {
          const n = new Set(s)
          n.delete(c.id)
          return n
        })
      } else {
        await api.addCircleMember(c.id, memberType, jid)
        setMemberIDs((s) => new Set(s).add(c.id))
      }
      onAdded()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy((b) => {
        const n = new Set(b)
        n.delete(c.id)
        return n
      })
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-950 p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3">
          <div className="text-sm font-semibold">Circles</div>
          <div dir="auto" className="truncate text-xs text-neutral-500">
            {chatTitle}
          </div>
        </div>
        {circles.length === 0 ? (
          <p className="py-6 text-center text-sm text-neutral-500">
            You don’t have any circles yet. Create one from the Circles tab.
          </p>
        ) : (
          <div className="flex max-h-[60vh] flex-col overflow-y-auto">
            {tree.map((node) => (
              <CircleTreeRow
                key={node.circle.id}
                node={node}
                depth={0}
                memberIDs={memberIDs || new Set()}
                busy={busy}
                onToggle={toggle}
              />
            ))}
          </div>
        )}
        {error && <div className="mt-2 text-xs text-red-400">{error}</div>}
        <div className="mt-3 text-right">
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

type CircleTreeNode = { circle: Circle; children: CircleTreeNode[] }

function buildCircleTree(circles: Circle[]): CircleTreeNode[] {
  const byID = new Map<number, Circle>()
  circles.forEach((c) => byID.set(c.id, c))
  const childrenOf = new Map<number, CircleTreeNode[]>()
  const roots: CircleTreeNode[] = []
  // Two-pass for stable ordering: first create empty nodes, then attach.
  const nodes = new Map<number, CircleTreeNode>()
  circles.forEach((c) => nodes.set(c.id, { circle: c, children: [] }))
  for (const c of circles) {
    const node = nodes.get(c.id)!
    const parents = (c.parent_ids || []).filter((id) => byID.has(id))
    if (parents.length === 0) {
      roots.push(node)
    } else {
      // First parent only — keeps the display a clean tree.
      const parentID = parents[0]
      const arr = childrenOf.get(parentID) || []
      arr.push(node)
      childrenOf.set(parentID, arr)
    }
  }
  for (const [parentID, kids] of childrenOf) {
    const parent = nodes.get(parentID)
    if (parent) parent.children = kids
  }
  return roots
}

function CircleTreeRow({
  node,
  depth,
  memberIDs,
  busy,
  onToggle,
}: {
  node: CircleTreeNode
  depth: number
  memberIDs: Set<number>
  busy: Set<number>
  onToggle: (c: Circle) => void
}) {
  const c = node.circle
  const isMember = memberIDs.has(c.id)
  const isBusy = busy.has(c.id)
  return (
    <>
      <button
        onClick={() => onToggle(c)}
        disabled={isBusy}
        style={{ paddingLeft: 8 + depth * 16 }}
        className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition hover:bg-neutral-900 disabled:opacity-50"
      >
        <span
          className={
            'flex h-4 w-4 shrink-0 items-center justify-center rounded border transition ' +
            (isMember
              ? 'border-emerald-500 bg-emerald-500 text-neutral-950'
              : 'border-neutral-600')
          }
        >
          {isMember ? '✓' : ''}
        </span>
        <span
          className="h-3 w-3 shrink-0 rounded-full"
          style={{ backgroundColor: c.color || '#737373' }}
        />
        <span dir="auto" className="min-w-0 flex-1 truncate text-neutral-200">
          {c.name}
        </span>
        {isBusy && <span className="text-xs text-neutral-500">…</span>}
      </button>
      {node.children.map((child) => (
        <CircleTreeRow
          key={child.circle.id}
          node={child}
          depth={depth + 1}
          memberIDs={memberIDs}
          busy={busy}
          onToggle={onToggle}
        />
      ))}
    </>
  )
}

// buildChatMenu returns the context menu items for one chat row. The menu is
// state-aware: a hidden chat (only seen in "private mode" view) gets Unhide
// instead of Hide; pinned/archived/muted/read toggle their inverse.
function buildChatMenu(
  chat: Chat | undefined,
  title: string,
  cb: {
    onOpen: (jid: string) => void
    onRequestHide: (jid: string, title: string) => void
    onChanged: () => void
    onRequestAddToCircle: (jid: string, title: string) => void
    onRequestMute: (jid: string, title: string) => void
    onRequestWallpaper: (jid: string, title: string) => void
  },
): MenuItem[] {
  if (!chat) return []
  const jid = chat.jid

  // Fire-and-refresh helper for chatAction calls.
  const act = async (action: string) => {
    try { await api.chatAction(jid, action) } catch {}
    cb.onChanged()
  }

  const items: MenuItem[] = [
    { label: 'Open chat', icon: '↗', onClick: () => cb.onOpen(jid) },
    { divider: true },
    {
      label: chat.unread_count > 0 ? 'Mark as read' : 'Mark as unread',
      icon: chat.unread_count > 0 ? '✓' : '●',
      onClick: () => act(chat.unread_count > 0 ? 'read' : 'unread'),
    },
    {
      label: chat.is_pinned ? 'Unpin' : 'Pin to top',
      icon: '📌',
      onClick: () => act(chat.is_pinned ? 'unpin' : 'pin'),
    },
    {
      label: chat.is_muted ? 'Unmute' : 'Mute…',
      icon: '🔇',
      // Mute opens the duration picker (8h / 1 week / Always — WA's
      // exact choices). Unmute is a single-step toggle so it stays
      // direct, no picker.
      onClick: () =>
        chat.is_muted ? act('unmute') : cb.onRequestMute(jid, title),
    },
    {
      label: chat.is_archived ? 'Unarchive' : 'Archive',
      icon: '📦',
      onClick: () => act(chat.is_archived ? 'unarchive' : 'archive'),
    },
    { divider: true },
    {
      label: 'Wallpaper…',
      icon: '🎨',
      onClick: () => cb.onRequestWallpaper(jid, title),
    },
    {
      label: 'Circles…',
      icon: '⭕',
      onClick: () => cb.onRequestAddToCircle(jid, title),
    },
    { divider: true },
  ]

  if (chat.is_hidden) {
    items.push({
      label: 'Unhide chat',
      icon: '🔓',
      onClick: async () => {
        try { await api.unhideChat(jid) } catch {}
        cb.onChanged()
      },
    })
  } else {
    items.push({
      label: 'Hide chat…',
      icon: '🔒',
      danger: true,
      onClick: () => cb.onRequestHide(jid, title),
    })
  }

  return items
}


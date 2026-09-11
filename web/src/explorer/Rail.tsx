import { useState } from 'react'

// The left navigation: WhatsApp's new labelled sidebar.
//
// It used to be a 68px icon-only rail. WhatsApp's desktop update replaced that
// with a wider column that spells each destination out, and the reason it reads
// better is not the labels alone — it is that labels make grouping legible. An
// icon strip can only separate groups with a hairline and hope you infer why;
// with text, "Chats / Updates / Calls" obviously belongs together, and a
// "More" section can fold away the things you rarely open.
//
// The grouping follows WA's own: conversation surfaces first, then the
// bridge's own features behind a divider (Circles, Focus, Tasks) so they read
// as native without pretending to be stock WhatsApp, then a collapsible More
// for filed things, and finally Settings and your profile pinned to the
// bottom.

export type RailItem =
  | 'chats'
  | 'calls'
  | 'status'
  | 'archived'
  | 'starred'
  | 'circles'
  | 'focus'
  | 'tasks'
  | 'meetings'

type Entry = {
  id: RailItem
  label: string
  icon: React.ReactNode
  badge?: number
  /** Small dot instead of a count — WA uses this for unseen status updates. */
  dot?: boolean
  /** Render the badge muted. Green reads as "unread"; a pile of archived
      chats is just a total, and should not shout like new messages do. */
  quiet?: boolean
}

const MORE_KEY = 'wa.sidebar.more'

const stroke = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.7,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function Icon({ children }: { children: React.ReactNode }) {
  return (
    <svg viewBox="0 0 24 24" width="21" height="21" {...stroke} aria-hidden="true">
      {children}
    </svg>
  )
}

export const RAIL_ICONS: Record<RailItem, React.ReactNode> = {
  chats: (
    <Icon>
      <path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 9 9 0 0 1-3.9-.9L3 20.5l1.6-4.6A8.4 8.4 0 0 1 12 3.1a8.4 8.4 0 0 1 9 8.4z" />
    </Icon>
  ),
  calls: (
    <Icon>
      <path d="M22 16.9v2.5a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 3.7 2 2 0 0 1 4.1 1.5h2.5a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1L7.7 9.3a16 16 0 0 0 6 6l1.2-1.1a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.7 2z" />
    </Icon>
  ),
  status: (
    <Icon>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 3a9 9 0 0 1 0 18M12 3a9 9 0 0 0 0 18" strokeDasharray="3 3" />
    </Icon>
  ),
  archived: (
    <Icon>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8M10 12h4" />
    </Icon>
  ),
  starred: (
    <Icon>
      <path d="M12 3.5l2.6 5.3 5.9.9-4.2 4.1 1 5.8-5.3-2.8-5.3 2.8 1-5.8L3.5 9.7l5.9-.9z" />
    </Icon>
  ),
  circles: (
    <Icon>
      <circle cx="9" cy="9" r="5" />
      <circle cx="15" cy="15" r="5" />
    </Icon>
  ),
  focus: (
    <Icon>
      <path d="M13 2.5L4.5 13.5H11l-1 8 8.5-11H12z" />
    </Icon>
  ),
  tasks: (
    <Icon>
      <rect x="3.5" y="4.5" width="17" height="15" rx="2.5" />
      <path d="M8 12l2.5 2.5L16 9" />
    </Icon>
  ),
  meetings: (
    <Icon>
      <rect x="3.5" y="5" width="17" height="15" rx="2.5" />
      <path d="M3.5 9.5h17M8 3.5v3M16 3.5v3" />
    </Icon>
  ),
}

const SETTINGS_ICON = (
  <Icon>
    <circle cx="12" cy="12" r="3.2" />
    <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.9 1.2v.2a2 2 0 1 1-4 0v-.1A1.7 1.7 0 0 0 7.9 19a1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0-1.2-2.9H1.9a2 2 0 1 1 0-4H2a1.7 1.7 0 0 0 1.6-1.1 1.7 1.7 0 0 0-.4-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3h.1A1.7 1.7 0 0 0 9 2.7v-.2a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.4l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9v.1a1.7 1.7 0 0 0 1.6 1H22a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
  </Icon>
)

// NavRow is one destination. Icon, label, and an optional count or dot — the
// same shape for every row so the column scans as a single list.
function NavRow({
  icon,
  label,
  active,
  badge,
  dot,
  quiet,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  active?: boolean
  badge?: number
  dot?: boolean
  quiet?: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active ? 'page' : undefined}
      className={
        'flex w-full items-center gap-3 rounded-lg py-2 pr-2 text-left text-sm transition ' +
        'pl-3 ' +
        (active
          ? 'bg-neutral-800 font-medium text-neutral-100'
          : 'text-neutral-300 hover:bg-neutral-800/60 hover:text-neutral-100')
      }
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {badge !== undefined && badge > 0 && (
        <span
          className={
            'shrink-0 rounded-full px-1.5 py-px text-[11px] font-semibold tabular-nums ' +
            (quiet ? 'bg-neutral-800 text-neutral-400' : 'bg-[#25d366] text-white')
          }
        >
          {badge > 99 ? '99+' : badge}
        </span>
      )}
      {dot && !badge && <span className="h-2 w-2 shrink-0 rounded-full bg-[#25d366]" />}
    </button>
  )
}

export default function Rail({
  active,
  onPick,
  onSettings,
  onProfile,
  unreadChats,
  archivedCount,
  openTasks,
  upcomingMeetings,
  hasStatusUpdates,
  profileName,
  avatar,
}: {
  active: RailItem
  onPick: (id: RailItem) => void
  onSettings: () => void
  onProfile?: () => void
  unreadChats?: number
  archivedCount?: number
  openTasks?: number
  /** Meetings still ahead — what the badge counts. */
  upcomingMeetings?: number
  hasStatusUpdates?: boolean
  profileName?: string
  avatar?: React.ReactNode
}) {
  // Whether "More" is open is remembered, because it is a preference about
  // your own layout, not app state. Storage can throw in a private window, so
  // a failure just means the default.
  const [moreOpen, setMoreOpen] = useState(() => {
    try {
      return localStorage.getItem(MORE_KEY) !== '0'
    } catch {
      return true
    }
  })

  function toggleMore() {
    setMoreOpen((open) => {
      const next = !open
      try {
        localStorage.setItem(MORE_KEY, next ? '1' : '0')
      } catch {
        /* not fatal — the section still toggles for this session */
      }
      return next
    })
  }

  const primary: Entry[] = [
    { id: 'chats', label: 'Chats', icon: RAIL_ICONS.chats, badge: unreadChats },
    { id: 'status', label: 'Updates', icon: RAIL_ICONS.status, dot: hasStatusUpdates },
    { id: 'calls', label: 'Calls', icon: RAIL_ICONS.calls },
  ]
  // Bridge-only surfaces. Grouped apart on purpose.
  const ours: Entry[] = [
    { id: 'circles', label: 'Circles', icon: RAIL_ICONS.circles },
    { id: 'meetings', label: 'Meetings', icon: RAIL_ICONS.meetings, badge: upcomingMeetings, quiet: true },
    { id: 'tasks', label: 'Tasks', icon: RAIL_ICONS.tasks, badge: openTasks },
  ]
  const filed: Entry[] = [
    { id: 'archived', label: 'Archived', icon: RAIL_ICONS.archived, badge: archivedCount, quiet: true },
    { id: 'starred', label: 'Starred', icon: RAIL_ICONS.starred },
  ]

  const rows = (entries: Entry[]) =>
    entries.map((e) => (
      <NavRow
        key={e.id}
        icon={e.icon}
        label={e.label}
        active={active === e.id}
        badge={e.badge}
        dot={e.dot}
        quiet={e.quiet}
        onClick={() => onPick(e.id)}
      />
    ))

  return (
    <nav
      aria-label="Main"
      className="flex w-[232px] shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-neutral-800 bg-wa-rail px-2 py-3"
    >
      {rows(primary)}
      <Divider />
      {rows(ours)}
      <Divider />

      <button
        onClick={toggleMore}
        aria-expanded={moreOpen}
        className="flex w-full items-center gap-2 rounded-lg py-1.5 pl-3 pr-2 text-left text-xs font-semibold text-neutral-500 transition hover:text-neutral-300"
      >
        <span className="min-w-0 flex-1">More</span>
        <svg
          viewBox="0 0 24 24"
          width="14"
          height="14"
          aria-hidden="true"
          className={'shrink-0 transition-transform ' + (moreOpen ? '' : '-rotate-90')}
          {...stroke}
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {moreOpen && rows(filed)}

      <div className="flex-1" />

      <NavRow icon={SETTINGS_ICON} label="Settings" onClick={onSettings} />
      {onProfile && (
        <button
          onClick={onProfile}
          className="flex w-full items-center gap-3 rounded-lg py-2 pl-3 pr-2 text-left text-sm text-neutral-300 transition hover:bg-neutral-800/60 hover:text-neutral-100"
        >
          <span className="flex h-[21px] w-[21px] shrink-0 items-center justify-center overflow-hidden rounded-full bg-neutral-700 text-[10px] font-semibold text-neutral-200">
            {avatar ?? (profileName || 'You').slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0 flex-1 truncate">{profileName || 'You'}</span>
        </button>
      )}
    </nav>
  )
}

function Divider() {
  return <div className="my-1 h-px bg-neutral-800" />
}

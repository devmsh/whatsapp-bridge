// Rail is WhatsApp's left icon column: the app's primary navigation.
//
// It follows WA's own grouping — a top group for conversation surfaces, a
// second group for saved/filed things, and settings pinned to the bottom. The
// bridge's own features (Circles, Focus, Tasks) sit in a third group with a
// divider above them, so they read as native without pretending to be part of
// stock WhatsApp.

export type RailItem =
  | 'chats'
  | 'calls'
  | 'status'
  | 'archived'
  | 'starred'
  | 'circles'
  | 'focus'
  | 'tasks'

type Entry = {
  id: RailItem
  label: string
  icon: React.ReactNode
  badge?: number
  /** Small dot instead of a count — WA uses this for unseen status updates. */
  dot?: boolean
}

const stroke = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.7,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function Icon({ children }: { children: React.ReactNode }) {
  return (
    <svg viewBox="0 0 24 24" width="23" height="23" {...stroke} aria-hidden="true">
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
}

function RailButton({
  entry,
  active,
  onPick,
}: {
  entry: Entry
  active: boolean
  onPick: (id: RailItem) => void
}) {
  return (
    <button
      onClick={() => onPick(entry.id)}
      title={entry.label}
      aria-label={entry.label}
      aria-current={active ? 'page' : undefined}
      className={
        'relative flex h-11 w-11 items-center justify-center rounded-full transition ' +
        (active
          ? 'bg-neutral-800 text-neutral-100'
          : 'text-neutral-400 hover:bg-neutral-800/70 hover:text-neutral-200')
      }
    >
      {entry.icon}
      {entry.badge !== undefined && entry.badge > 0 && (
        <span className="absolute -right-0.5 -top-0.5 flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-[#25d366] px-1 text-[10px] font-semibold tabular-nums text-white">
          {entry.badge > 99 ? '99+' : entry.badge}
        </span>
      )}
      {entry.dot && !entry.badge && (
        <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-[#25d366] ring-2 ring-[#f4f4f4]" />
      )}
    </button>
  )
}

export default function Rail({
  active,
  onPick,
  onSettings,
  unreadChats,
  archivedCount,
  openTasks,
  hasStatusUpdates,
  avatar,
}: {
  active: RailItem
  onPick: (id: RailItem) => void
  onSettings: () => void
  unreadChats?: number
  archivedCount?: number
  openTasks?: number
  hasStatusUpdates?: boolean
  avatar?: React.ReactNode
}) {
  // WA's three groups, separated by hairlines.
  const primary: Entry[] = [
    { id: 'chats', label: 'Chats', icon: RAIL_ICONS.chats, badge: unreadChats },
    { id: 'calls', label: 'Calls', icon: RAIL_ICONS.calls },
    { id: 'status', label: 'Status', icon: RAIL_ICONS.status, dot: hasStatusUpdates },
  ]
  const filed: Entry[] = [
    { id: 'archived', label: 'Archived', icon: RAIL_ICONS.archived, badge: archivedCount },
    { id: 'starred', label: 'Starred', icon: RAIL_ICONS.starred },
  ]
  // Bridge-only surfaces. Grouped apart on purpose.
  const ours: Entry[] = [
    { id: 'circles', label: 'Circles', icon: RAIL_ICONS.circles },
    { id: 'focus', label: 'Focus Mode', icon: RAIL_ICONS.focus },
    { id: 'tasks', label: 'Tasks', icon: RAIL_ICONS.tasks, badge: openTasks },
  ]

  const group = (entries: Entry[]) =>
    entries.map((e) => (
      <RailButton key={e.id} entry={e} active={active === e.id} onPick={onPick} />
    ))

  return (
    <nav
      aria-label="Main"
      className="flex w-[68px] shrink-0 flex-col items-center gap-1 border-r border-neutral-800 bg-[#f4f4f4] py-3"
    >
      {group(primary)}
      <Divider />
      {group(filed)}
      <Divider />
      {group(ours)}

      <div className="flex-1" />

      <button
        onClick={onSettings}
        title="Settings"
        aria-label="Settings"
        className="flex h-11 w-11 items-center justify-center rounded-full text-neutral-400 transition hover:bg-neutral-800/70 hover:text-neutral-200"
      >
        <Icon>
          <circle cx="12" cy="12" r="3.2" />
          <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.9 1.2v.2a2 2 0 1 1-4 0v-.1A1.7 1.7 0 0 0 7.9 19a1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0-1.2-2.9H1.9a2 2 0 1 1 0-4H2a1.7 1.7 0 0 0 1.6-1.1 1.7 1.7 0 0 0-.4-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3h.1A1.7 1.7 0 0 0 9 2.7v-.2a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.4l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9v.1a1.7 1.7 0 0 0 1.6 1H22a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
        </Icon>
      </button>

      {avatar && <div className="mt-1 pb-1">{avatar}</div>}
    </nav>
  )
}

function Divider() {
  return <div className="my-1.5 h-px w-8 bg-neutral-700" />
}

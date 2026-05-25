import { useMemo } from 'react'
import type { Circle, Task } from '../api'

// A "quick view" filter — the small saved views at the top of the sidebar.
export type QuickView =
  | 'open'        // all open + in_progress
  | 'high'        // open + priority=high
  | 'overdue'     // due_at < now, not done
  | 'today'       // due_at within today
  | 'mine'        // assigned to me (resolved via ownJID)
  | 'recent'      // created in the last 24h

// Selection type passed to onSelect — either a quick view OR a circle id.
export type TasksSelection = { kind: 'view'; view: QuickView } | { kind: 'circle'; id: number }

// TasksSidebar is the left rail when the Tasks tab is active. It shows quick
// saved views with task counts plus per-circle counts, each clickable to
// scope the main view.
export function TasksSidebar({
  tasks,
  circles,
  ownJID,
  selected,
  onSelect,
}: {
  tasks: Task[]
  circles: Circle[]
  ownJID: string
  selected: TasksSelection
  onSelect: (s: TasksSelection) => void
}) {
  const counts = useMemo(() => computeCounts(tasks, circles, ownJID), [tasks, circles, ownJID])

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto py-2">
      <Section label="Quick views">
        <Row
          label="All open"
          icon="📋"
          count={counts.view.open}
          active={selected.kind === 'view' && selected.view === 'open'}
          onClick={() => onSelect({ kind: 'view', view: 'open' })}
        />
        <Row
          label="High priority"
          icon="🔴"
          count={counts.view.high}
          active={selected.kind === 'view' && selected.view === 'high'}
          onClick={() => onSelect({ kind: 'view', view: 'high' })}
        />
        <Row
          label="Overdue"
          icon="⏰"
          count={counts.view.overdue}
          danger
          active={selected.kind === 'view' && selected.view === 'overdue'}
          onClick={() => onSelect({ kind: 'view', view: 'overdue' })}
        />
        <Row
          label="Due today"
          icon="📅"
          count={counts.view.today}
          active={selected.kind === 'view' && selected.view === 'today'}
          onClick={() => onSelect({ kind: 'view', view: 'today' })}
        />
        <Row
          label="Assigned to me"
          icon="👤"
          count={counts.view.mine}
          active={selected.kind === 'view' && selected.view === 'mine'}
          onClick={() => onSelect({ kind: 'view', view: 'mine' })}
        />
        <Row
          label="New today"
          icon="✨"
          count={counts.view.recent}
          active={selected.kind === 'view' && selected.view === 'recent'}
          onClick={() => onSelect({ kind: 'view', view: 'recent' })}
        />
      </Section>

      <Section label="By circle">
        {circles
          .map((c) => ({ c, n: counts.byCircle.get(c.id) || 0 }))
          .sort((a, b) => b.n - a.n)
          .map(({ c, n }) => (
            <Row
              key={c.id}
              label={c.name}
              dotColor={c.color || '#737373'}
              count={n}
              active={selected.kind === 'circle' && selected.id === c.id}
              onClick={() => onSelect({ kind: 'circle', id: c.id })}
            />
          ))}
      </Section>
    </div>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mb-3">
      <div className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        {label}
      </div>
      {children}
    </div>
  )
}

function Row({
  label,
  icon,
  dotColor,
  count,
  active,
  danger,
  onClick,
}: {
  label: string
  icon?: string
  dotColor?: string
  count: number
  active?: boolean
  danger?: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition ' +
        (active ? 'bg-neutral-800 text-neutral-100' : 'text-neutral-300 hover:bg-neutral-900')
      }
    >
      {icon ? (
        <span className="w-4 shrink-0 text-center">{icon}</span>
      ) : (
        <span
          className="h-2.5 w-2.5 shrink-0 rounded-full"
          style={{ backgroundColor: dotColor }}
        />
      )}
      <span dir="auto" className="min-w-0 flex-1 truncate">
        {label}
      </span>
      {count > 0 && (
        <span
          className={
            'shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-semibold ' +
            (danger
              ? 'bg-red-500/20 text-red-300'
              : active
                ? 'bg-neutral-700 text-neutral-200'
                : 'bg-neutral-800 text-neutral-400')
          }
        >
          {count}
        </span>
      )}
    </button>
  )
}

// computeCounts derives every sidebar count from one pass over the task list.
// Counts only consider tasks the user would actually see (review_status !=
// rejected). "open" means status open or in_progress.
function computeCounts(tasks: Task[], circles: Circle[], ownJID: string) {
  const now = Math.floor(Date.now() / 1000)
  const dayStart = Math.floor(new Date().setHours(0, 0, 0, 0) / 1000)
  const dayEnd = dayStart + 86400
  const last24h = now - 86400

  const v = { open: 0, high: 0, overdue: 0, today: 0, mine: 0, recent: 0 }
  const byCircle = new Map<number, number>()
  for (const c of circles) byCircle.set(c.id, 0)

  for (const t of tasks) {
    if (t.review_status === 'rejected') continue
    const isOpen = t.status === 'open' || t.status === 'in_progress'

    if (isOpen) v.open++
    if (isOpen && t.priority === 'high') v.high++
    if (isOpen && t.due_at > 0 && t.due_at < now) v.overdue++
    if (isOpen && t.due_at >= dayStart && t.due_at < dayEnd) v.today++
    if (isOpen && ownJID && t.assignee_jid === ownJID) v.mine++
    if (t.created_at >= last24h) v.recent++

    if (isOpen) {
      for (const cid of t.circle_ids || []) {
        byCircle.set(cid, (byCircle.get(cid) || 0) + 1)
      }
    }
  }
  return { view: v, byCircle }
}

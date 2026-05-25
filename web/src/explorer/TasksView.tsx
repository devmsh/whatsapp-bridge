import { useMemo, useState } from 'react'
import { api, type Chat, type Circle, type Task, type TaskStatus } from '../api'
import type { TasksSelection } from './TasksSidebar'
import { chatTitle, jidUser } from './format'
import { ChatAvatar } from './ChatAvatar'

const STATUS_DOT: Record<TaskStatus, string> = {
  open: '#64748b',
  in_progress: '#3b82f6',
  done: '#22c55e',
  cancelled: '#737373',
}

// TasksView is the rich main-view list. The header reflects the current scope
// (which sidebar item is selected) and exposes status filters; the body is
// either grouped by circle (when scope is broad) or flat (when scope IS a
// single circle). Parents render their children indented as part of the same
// card so the 2-level hierarchy stays visible.
export function TasksView({
  tasks,
  circles,
  chats,
  nameMap,
  ownJID,
  selection,
  onOpenTask,
  onCreated,
  onChanged,
}: {
  tasks: Task[]
  circles: Circle[]
  chats: Chat[]
  nameMap: Map<string, string>
  ownJID: string
  selection: TasksSelection
  onOpenTask: (id: number) => void
  onCreated: () => void
  onChanged: () => void
}) {
  const [statusFilter, setStatusFilter] = useState<'all' | 'open' | 'in_progress' | 'done'>('open')
  const [creating, setCreating] = useState(false)
  const [newTitle, setNewTitle] = useState('')
  const [busy, setBusy] = useState(false)

  // Scope: filter tasks by the sidebar selection.
  const scoped = useMemo(
    () => filterByScope(tasks, selection, ownJID),
    [tasks, selection, ownJID],
  )

  // Apply the status pill filter on top of the scope.
  const filtered = useMemo(() => {
    const open = (t: Task) => t.status === 'open' || t.status === 'in_progress'
    return scoped.filter((t) => {
      if (t.review_status === 'rejected') return false
      if (statusFilter === 'all') return true
      if (statusFilter === 'open') return open(t)
      return t.status === statusFilter
    })
  }, [scoped, statusFilter])

  // Group by circle when scope is a broad view; flat when scope is a circle.
  const grouped = useMemo(() => groupForRender(filtered, circles, selection), [filtered, circles, selection])
  const heading = describeSelection(selection, circles)
  const tasksScopedCount = scoped.length

  async function createTask() {
    if (!newTitle.trim() || busy) return
    setBusy(true)
    try {
      const t = await api.createTask({
        title: newTitle.trim(),
        circle_id: selection.kind === 'circle' ? selection.id : undefined,
      })
      setNewTitle('')
      setCreating(false)
      onCreated()
      onOpenTask(t.id)
    } finally {
      setBusy(false)
    }
  }

  async function toggleDone(t: Task) {
    const next: TaskStatus = t.status === 'done' ? 'open' : 'done'
    await api.updateTask(t.id, { status: next })
    onChanged()
  }

  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-neutral-800 px-6 py-3">
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold">{heading.title}</div>
            <div className="text-xs text-neutral-500">
              {tasksScopedCount} task{tasksScopedCount === 1 ? '' : 's'} · {heading.hint}
            </div>
          </div>
          <button
            onClick={() => setCreating(true)}
            className="shrink-0 rounded-lg bg-emerald-500 px-3 py-1.5 text-sm font-medium text-neutral-950 hover:bg-emerald-400"
          >
            + New
          </button>
        </div>

        <div className="mt-3 flex gap-1.5 text-xs">
          {(['open', 'in_progress', 'done', 'all'] as const).map((s) => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              className={
                'rounded-full px-2.5 py-1 transition ' +
                (statusFilter === s
                  ? 'bg-emerald-500 text-neutral-950'
                  : 'bg-neutral-800 text-neutral-300 hover:bg-neutral-700')
              }
            >
              {s === 'in_progress' ? 'Doing' : s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>

        {creating && (
          <div className="mt-3 flex gap-2">
            <input
              autoFocus
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') createTask()
                if (e.key === 'Escape') {
                  setCreating(false)
                  setNewTitle('')
                }
              }}
              placeholder="Task title"
              className="flex-1 rounded-lg border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={createTask}
              disabled={busy || !newTitle.trim()}
              className="rounded-lg bg-emerald-500 px-3 text-sm font-medium text-neutral-950 disabled:opacity-50"
            >
              Add
            </button>
            <button
              onClick={() => {
                setCreating(false)
                setNewTitle('')
              }}
              className="rounded-lg border border-neutral-700 px-3 text-sm text-neutral-300 hover:bg-neutral-800"
            >
              Cancel
            </button>
          </div>
        )}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
        {grouped.length === 0 ? (
          <div className="py-16 text-center text-sm text-neutral-600">No tasks in view.</div>
        ) : (
          <div className="flex flex-col gap-5">
            {grouped.map((group) => (
              <section key={group.key}>
                {group.label && (
                  <div className="mb-2 flex items-baseline gap-2 text-[11px] uppercase tracking-wide">
                    {group.color && (
                      <span
                        className="h-2 w-2 rounded-full"
                        style={{ backgroundColor: group.color }}
                      />
                    )}
                    <span className="font-semibold text-neutral-400">{group.label}</span>
                    <span className="text-neutral-600">· {group.items.length}</span>
                  </div>
                )}
                <div className="flex flex-col gap-2">
                  {group.items.map((entry) => (
                    <TaskCard
                      key={entry.parent.id}
                      entry={entry}
                      chats={chats}
                      nameMap={nameMap}
                      onOpen={onOpenTask}
                      onToggleDone={toggleDone}
                    />
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────

// TaskCard: a parent task (possibly with children indented inside the same
// card) or a standalone task.
function TaskCard({
  entry,
  chats,
  nameMap,
  onOpen,
  onToggleDone,
}: {
  entry: ParentEntry
  chats: Chat[]
  nameMap: Map<string, string>
  onOpen: (id: number) => void
  onToggleDone: (t: Task) => void
}) {
  const t = entry.parent
  const isAIPending = t.review_status === 'pending_review'
  const isOverdue = t.due_at > 0 && t.due_at < Math.floor(Date.now() / 1000) && t.status !== 'done'

  return (
    <div className="rounded-xl border border-neutral-800 bg-neutral-900/30 transition hover:border-neutral-700">
      <TaskRow
        t={t}
        depth={0}
        isParent={entry.children.length > 0}
        isAIPending={isAIPending}
        isOverdue={isOverdue}
        chats={chats}
        nameMap={nameMap}
        onOpen={onOpen}
        onToggleDone={onToggleDone}
      />
      {entry.children.length > 0 && (
        <div className="border-t border-neutral-800/60">
          {entry.children.map((child) => (
            <TaskRow
              key={child.id}
              t={child}
              depth={1}
              isParent={false}
              isAIPending={child.review_status === 'pending_review'}
              isOverdue={
                child.due_at > 0 &&
                child.due_at < Math.floor(Date.now() / 1000) &&
                child.status !== 'done'
              }
              chats={chats}
              nameMap={nameMap}
              onOpen={onOpen}
              onToggleDone={onToggleDone}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function TaskRow({
  t,
  depth,
  isParent,
  isAIPending,
  isOverdue,
  chats,
  nameMap,
  onOpen,
  onToggleDone,
}: {
  t: Task
  depth: 0 | 1
  isParent: boolean
  isAIPending: boolean
  isOverdue: boolean
  chats: Chat[]
  nameMap: Map<string, string>
  onOpen: (id: number) => void
  onToggleDone: (t: Task) => void
}) {
  const done = t.status === 'done' || t.status === 'cancelled'
  const assigneeName = t.assignee_jid
    ? nameMap.get(t.assignee_jid) || '+' + jidUser(t.assignee_jid)
    : ''
  const originChat = chats.find((c) => c.jid === t.origin_chat_jid)
  return (
    <div
      className={
        'group flex items-center gap-3 px-4 py-2.5 transition hover:bg-neutral-900/50 ' +
        (depth === 1 ? 'pl-10' : '')
      }
    >
      {/* checkbox-style done toggle */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onToggleDone(t)
        }}
        title={done ? 'Reopen' : 'Mark done'}
        className={
          'flex h-5 w-5 shrink-0 items-center justify-center rounded border transition ' +
          (done
            ? 'border-emerald-500 bg-emerald-500 text-neutral-950'
            : 'border-neutral-600 hover:border-emerald-500 hover:bg-emerald-500/10')
        }
      >
        {done ? '✓' : ''}
      </button>

      {/* Clickable area */}
      <button onClick={() => onOpen(t.id)} className="flex min-w-0 flex-1 items-center gap-3 text-left">
        <span
          className="h-2 w-2 shrink-0 rounded-full"
          style={{ backgroundColor: STATUS_DOT[t.status] }}
        />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            {isAIPending && (
              <Chip color="amber" mono>
                ✨ AI
              </Chip>
            )}
            {t.priority === 'high' && <Chip color="red">HIGH</Chip>}
            {isParent && <Chip color="sky">★ {/* parent marker */}</Chip>}
            <span
              dir="auto"
              className={
                'block min-w-0 flex-1 truncate text-sm ' +
                (isParent ? 'font-semibold text-neutral-100 ' : 'text-neutral-200 ') +
                (done ? 'line-through text-neutral-500' : '')
              }
            >
              {t.title}
            </span>
          </span>
          <span className="mt-0.5 flex items-center gap-2 text-[11px] text-neutral-500">
            {assigneeName && <span className="truncate">@{assigneeName.split(' ')[0]}</span>}
            {t.due_at > 0 && (
              <span className={isOverdue ? 'text-red-400' : ''}>
                {isOverdue ? '⏰ ' : '📅 '}
                {new Date(t.due_at * 1000).toLocaleDateString()}
              </span>
            )}
            {t.message_count > 0 && <span>💬 {t.message_count}</span>}
          </span>
        </span>
      </button>

      {/* Origin chat shortcut */}
      {originChat && (
        <div title={'From: ' + chatTitle(originChat, nameMap)} className="shrink-0">
          <ChatAvatar
            jid={originChat.jid}
            title={chatTitle(originChat, nameMap)}
            group={originChat.jid.endsWith('@g.us')}
            size={22}
          />
        </div>
      )}
    </div>
  )
}

function Chip({
  color,
  mono,
  children,
}: {
  color: 'amber' | 'red' | 'sky' | 'neutral'
  mono?: boolean
  children: React.ReactNode
}) {
  const cls: Record<typeof color, string> = {
    amber: 'bg-amber-500/20 text-amber-300',
    red: 'bg-red-500/20 text-red-300',
    sky: 'bg-sky-500/20 text-sky-300',
    neutral: 'bg-neutral-800 text-neutral-300',
  }
  return (
    <span
      className={
        'rounded px-1.5 py-0.5 text-[10px] font-semibold ' + cls[color] + (mono ? ' font-mono' : '')
      }
    >
      {children}
    </span>
  )
}

// ─── grouping + filtering ────────────────────────────────────────────

type ParentEntry = { parent: Task; children: Task[] }
type Group = { key: string; label?: string; color?: string; items: ParentEntry[] }

// filterByScope narrows a task list to the sidebar's current selection.
function filterByScope(tasks: Task[], selection: TasksSelection, ownJID: string): Task[] {
  const now = Math.floor(Date.now() / 1000)
  const dayStart = Math.floor(new Date().setHours(0, 0, 0, 0) / 1000)
  const dayEnd = dayStart + 86400
  const last24h = now - 86400
  const isOpen = (t: Task) => t.status === 'open' || t.status === 'in_progress'

  if (selection.kind === 'circle') {
    return tasks.filter((t) => (t.circle_ids || []).includes(selection.id))
  }
  switch (selection.view) {
    case 'open':
      return tasks.filter(isOpen)
    case 'high':
      return tasks.filter((t) => isOpen(t) && t.priority === 'high')
    case 'overdue':
      return tasks.filter((t) => isOpen(t) && t.due_at > 0 && t.due_at < now)
    case 'today':
      return tasks.filter((t) => isOpen(t) && t.due_at >= dayStart && t.due_at < dayEnd)
    case 'mine':
      return tasks.filter((t) => isOpen(t) && ownJID && t.assignee_jid === ownJID)
    case 'recent':
      return tasks.filter((t) => t.created_at >= last24h)
    default:
      return tasks
  }
}

// groupForRender produces sections of parent/child trees.
// • If scope IS a single circle, render one section with no header.
// • Otherwise group by primary circle; tasks in no circle land in "Other".
function groupForRender(
  tasks: Task[],
  circles: Circle[],
  selection: TasksSelection,
): Group[] {
  // First build parent/child entries (2-level).
  const byId = new Map<number, Task>()
  tasks.forEach((t) => byId.set(t.id, t))
  const childrenOf = new Map<number, Task[]>()
  const standalone: Task[] = []
  for (const t of tasks) {
    if (t.parent_id && byId.has(t.parent_id)) {
      const arr = childrenOf.get(t.parent_id) || []
      arr.push(t)
      childrenOf.set(t.parent_id, arr)
    }
  }
  const entries: ParentEntry[] = []
  for (const t of tasks) {
    if (t.parent_id && byId.has(t.parent_id)) continue
    entries.push({ parent: t, children: childrenOf.get(t.id) || [] })
  }
  // Sort entries: parents first (more children → higher), then by priority/recency.
  entries.sort((a, b) => {
    const aP = a.children.length > 0 ? 1 : 0
    const bP = b.children.length > 0 ? 1 : 0
    if (aP !== bP) return bP - aP
    const aPrio = a.parent.priority === 'high' ? 0 : 1
    const bPrio = b.parent.priority === 'high' ? 0 : 1
    if (aPrio !== bPrio) return aPrio - bPrio
    return (b.parent.updated_at || 0) - (a.parent.updated_at || 0)
  })

  if (selection.kind === 'circle') {
    return [{ key: 'flat', items: entries }]
  }

  // Group by primary circle.
  const groups = new Map<string, ParentEntry[]>()
  const circleById = new Map(circles.map((c) => [c.id, c]))
  for (const e of entries) {
    const cid = e.parent.circle_ids?.[0]
    const key = cid != null ? String(cid) : 'other'
    const arr = groups.get(key) || []
    arr.push(e)
    groups.set(key, arr)
  }
  const sorted: Group[] = []
  for (const [key, items] of groups) {
    if (key === 'other') continue
    const c = circleById.get(parseInt(key, 10))
    sorted.push({ key, label: c?.name || 'Circle', color: c?.color, items })
  }
  sorted.sort((a, b) => b.items.length - a.items.length)
  const others = groups.get('other')
  if (others?.length) sorted.push({ key: 'other', label: 'No circle', items: others })
  return sorted
  void standalone // (intentionally unused — entries already covers standalones)
}

function describeSelection(s: TasksSelection, circles: Circle[]): { title: string; hint: string } {
  if (s.kind === 'circle') {
    const c = circles.find((c) => c.id === s.id)
    return { title: c ? c.name : 'Circle', hint: 'tasks in this circle' }
  }
  const titles: Record<typeof s.view, string> = {
    open: 'All open tasks',
    high: 'High priority',
    overdue: 'Overdue',
    today: 'Due today',
    mine: 'Assigned to me',
    recent: 'New today',
  }
  const hints: Record<typeof s.view, string> = {
    open: 'open + in progress',
    high: 'open + high priority',
    overdue: 'past due date',
    today: 'due before midnight',
    mine: 'assigned to your account',
    recent: 'created in the last 24h',
  }
  return { title: titles[s.view], hint: hints[s.view] }
}

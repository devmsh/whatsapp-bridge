import { useEffect, useMemo, useState } from 'react'
import { api, type Task, type TaskReviewStatus, type TaskStatus } from '../api'
import { chatTitle, jidUser } from './format'
import type { Chat } from '../api'

const STATUS_FILTERS: { key: string; label: string }[] = [
  { key: 'open', label: 'Open' },
  { key: 'in_progress', label: 'Doing' },
  { key: 'done', label: 'Done' },
  { key: '', label: 'All' },
]

const DOT: Record<TaskStatus, string> = {
  open: '#64748b',
  in_progress: '#3b82f6',
  done: '#22c55e',
  cancelled: '#737373',
}

// TasksPanel is the sidebar list of tasks, optionally scoped to a chat or circle.
export function TasksPanel({
  chats,
  nameMap,
  chatFilter,
  circleFilter,
  circleName,
  selected,
  version,
  onOpen,
  onCreated,
  onClearFilter,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  chatFilter: string | null
  circleFilter: number | null
  circleName: string
  selected: number | null
  version: number
  onOpen: (id: number) => void
  onCreated: () => void
  onClearFilter: () => void
}) {
  const [tasks, setTasks] = useState<Task[]>([])
  const [status, setStatus] = useState('open')
  const [reviewMode, setReviewMode] = useState(false) // when true, show only pending_review
  const [creating, setCreating] = useState(false)
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)
  // local override of review_status so the row disappears immediately after action
  const [localReview, setLocalReview] = useState<Record<number, TaskReviewStatus>>({})

  const scoped = chatFilter || circleFilter != null

  // The displayed list. Server returns everything in scope; we filter in JS
  // so we can also use the local override for instant feedback.
  const visible = useMemo(() => {
    const withLocal = tasks.map((t) =>
      localReview[t.id] ? { ...t, review_status: localReview[t.id] } : t,
    )
    if (reviewMode) return withLocal.filter((t) => t.review_status === 'pending_review')
    // default list: hide rejected, and (outside review mode) hide pending unless scoped
    return withLocal.filter(
      (t) =>
        t.review_status !== 'rejected' &&
        (scoped || t.review_status !== 'pending_review'),
    )
  }, [tasks, localReview, reviewMode, scoped])

  const pendingCount = useMemo(
    () =>
      tasks.filter(
        (t) => (localReview[t.id] || t.review_status) === 'pending_review',
      ).length,
    [tasks, localReview],
  )

  useEffect(() => {
    api
      .tasks({
        status: scoped || reviewMode ? '' : status,
        chat: chatFilter || undefined,
        circle: circleFilter || undefined,
      })
      .then((t) => {
        setTasks(t || [])
        setLocalReview({})
      })
      .catch(() => setTasks([]))
  }, [status, chatFilter, circleFilter, version, scoped, reviewMode])

  async function setReview(id: number, review: TaskReviewStatus) {
    setLocalReview((prev) => ({ ...prev, [id]: review })) // optimistic
    try {
      await api.reviewTask(id, review)
    } catch {
      setLocalReview((prev) => {
        const c = { ...prev }
        delete c[id]
        return c
      })
    }
  }

  async function acceptAllPending() {
    const ids = visible.map((t) => t.id)
    setLocalReview((prev) => {
      const c = { ...prev }
      ids.forEach((id) => (c[id] = 'accepted'))
      return c
    })
    for (const id of ids) {
      try { await api.reviewTask(id, 'accepted') } catch {}
    }
  }

  const filterLabel = useMemo(() => {
    if (chatFilter) {
      const c = chats.find((ch) => ch.jid === chatFilter)
      return c ? chatTitle(c, nameMap) : '+' + jidUser(chatFilter)
    }
    if (circleFilter != null) return circleName
    return ''
  }, [chatFilter, circleFilter, circleName, chats, nameMap])

  async function create() {
    const t = title.trim()
    if (!t || busy) return
    setBusy(true)
    try {
      const task = await api.createTask({
        title: t,
        origin_chat_jid: chatFilter || undefined,
        circle_id: circleFilter || undefined,
      })
      setTitle('')
      setCreating(false)
      onCreated()
      onOpen(task.id)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {scoped && (
        <div className="flex items-center justify-between border-b border-neutral-800 px-3 py-2 text-xs">
          <span dir="auto" className="min-w-0 truncate text-neutral-400">
            Tasks in <span className="text-neutral-200">{filterLabel}</span>
          </span>
          <button onClick={onClearFilter} className="shrink-0 text-neutral-500 hover:text-neutral-200">
            All tasks
          </button>
        </div>
      )}

      {!scoped && (
        <div className="flex flex-wrap items-center gap-1 px-2 pt-2 text-xs">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.key}
              onClick={() => {
                setStatus(f.key)
                setReviewMode(false)
              }}
              className={
                'rounded-full px-2.5 py-1 transition ' +
                (!reviewMode && status === f.key
                  ? 'bg-emerald-500 text-neutral-950'
                  : 'bg-neutral-800 text-neutral-300')
              }
            >
              {f.label}
            </button>
          ))}
          {pendingCount > 0 && (
            <button
              onClick={() => setReviewMode((m) => !m)}
              className={
                'ml-auto rounded-full px-2.5 py-1 transition ' +
                (reviewMode
                  ? 'bg-amber-500 text-neutral-950'
                  : 'bg-amber-500/15 text-amber-300 hover:bg-amber-500/30')
              }
              title="AI-extracted tasks waiting for your review"
            >
              ✨ Review · {pendingCount}
            </button>
          )}
        </div>
      )}

      {reviewMode && visible.length > 0 && (
        <div className="border-y border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs">
          <button
            onClick={acceptAllPending}
            className="rounded bg-emerald-500/20 px-2 py-1 font-medium text-emerald-300 hover:bg-emerald-500/30"
          >
            ✓ Accept all visible ({visible.length})
          </button>
        </div>
      )}

      <div className="p-2">
        {creating ? (
          <div className="flex gap-2">
            <input
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') create()
                if (e.key === 'Escape') setCreating(false)
              }}
              placeholder="Task title"
              className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={create}
              disabled={busy}
              className="rounded-lg bg-emerald-500 px-3 text-sm font-medium text-neutral-950 disabled:opacity-50"
            >
              Add
            </button>
          </div>
        ) : (
          <button
            onClick={() => setCreating(true)}
            className="w-full rounded-lg border border-dashed border-neutral-700 px-3 py-2 text-sm text-neutral-400 transition hover:border-neutral-500 hover:text-neutral-200"
          >
            + New task
          </button>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {visible.length === 0 && (
          <div className="p-4 text-center text-xs text-neutral-600">
            {reviewMode ? 'No tasks waiting for review.' : 'No tasks'}
          </div>
        )}
        {visible.map((t) => {
          const isPending = t.review_status === 'pending_review'
          return (
            <div
              key={t.id}
              className={
                'group relative flex items-start gap-3 border-b border-neutral-900 px-3 py-2.5 transition ' +
                (selected === t.id ? 'bg-neutral-800' : 'hover:bg-neutral-900')
              }
            >
              <button
                onClick={() => onOpen(t.id)}
                className="flex min-w-0 flex-1 items-start gap-3 text-left"
              >
                <span
                  className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full"
                  style={{ backgroundColor: DOT[t.status] }}
                />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    {isPending && (
                      <span className="rounded bg-amber-500/20 px-1 py-0.5 text-[10px] font-semibold text-amber-300">
                        ✨ AI
                      </span>
                    )}
                    <span
                      dir="auto"
                      className={
                        'block min-w-0 flex-1 truncate text-sm ' +
                        (t.status === 'done' || t.status === 'cancelled'
                          ? 'text-neutral-500 line-through'
                          : '')
                      }
                    >
                      {t.title}
                    </span>
                  </span>
                  <span className="block truncate text-xs text-neutral-500">
                    {t.assignee_jid
                      ? (nameMap.get(t.assignee_jid) || '+' + jidUser(t.assignee_jid)) + ' · '
                      : ''}
                    {t.due_at
                      ? 'due ' + new Date(t.due_at * 1000).toLocaleDateString()
                      : 'no due date'}
                    {t.message_count > 0 ? ` · ${t.message_count} linked` : ''}
                  </span>
                </span>
              </button>
              {isPending && (
                <div className="flex shrink-0 items-center gap-1 pt-1">
                  <button
                    onClick={() => setReview(t.id, 'accepted')}
                    title="Accept"
                    className="rounded bg-emerald-500/20 px-1.5 py-0.5 text-xs text-emerald-300 hover:bg-emerald-500/40"
                  >
                    ✓
                  </button>
                  <button
                    onClick={() => setReview(t.id, 'rejected')}
                    title="Reject (not a real task)"
                    className="rounded bg-red-500/15 px-1.5 py-0.5 text-xs text-red-300 hover:bg-red-500/30"
                  >
                    ✗
                  </button>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

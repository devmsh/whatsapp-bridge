import { useEffect, useMemo, useState } from 'react'
import { api, type Task, type TaskStatus } from '../api'
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
  const [creating, setCreating] = useState(false)
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)

  const scoped = chatFilter || circleFilter != null

  useEffect(() => {
    api
      .tasks({
        status: scoped ? '' : status,
        chat: chatFilter || undefined,
        circle: circleFilter || undefined,
      })
      .then((t) => setTasks(t || []))
      .catch(() => setTasks([]))
  }, [status, chatFilter, circleFilter, version, scoped])

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
        <div className="flex gap-1 px-2 pt-2 text-xs">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.key}
              onClick={() => setStatus(f.key)}
              className={
                'rounded-full px-2.5 py-1 transition ' +
                (status === f.key ? 'bg-emerald-500 text-neutral-950' : 'bg-neutral-800 text-neutral-300')
              }
            >
              {f.label}
            </button>
          ))}
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
        {tasks.length === 0 && (
          <div className="p-4 text-center text-xs text-neutral-600">No tasks</div>
        )}
        {tasks.map((t) => (
          <button
            key={t.id}
            onClick={() => onOpen(t.id)}
            className={
              'flex w-full items-start gap-3 px-3 py-2.5 text-left transition ' +
              (selected === t.id ? 'bg-neutral-800' : 'hover:bg-neutral-900')
            }
          >
            <span
              className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: DOT[t.status] }}
            />
            <span className="min-w-0 flex-1">
              <span
                dir="auto"
                className={
                  'block truncate text-sm ' +
                  (t.status === 'done' || t.status === 'cancelled'
                    ? 'text-neutral-500 line-through'
                    : '')
                }
              >
                {t.title}
              </span>
              <span className="block truncate text-xs text-neutral-500">
                {t.assignee_jid
                  ? (nameMap.get(t.assignee_jid) || '+' + jidUser(t.assignee_jid)) + ' · '
                  : ''}
                {t.due_at ? 'due ' + new Date(t.due_at * 1000).toLocaleDateString() : 'no due date'}
                {t.message_count > 0 ? ` · ${t.message_count} linked` : ''}
              </span>
            </span>
          </button>
        ))}
      </div>
    </div>
  )
}

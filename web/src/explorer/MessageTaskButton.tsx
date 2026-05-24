import { useState } from 'react'
import { api, type Task } from '../api'

const LINK_ROLES = ['comment', 'completion', 'attachment', 'related']

// MessageTaskButton is the ✓ action on a message: create a task from it (origin)
// or link it to an existing task (as comment/completion/attachment) — which is
// how a task spans multiple chats.
export function MessageTaskButton({
  chatJID,
  messageID,
  defaultTitle,
  onOpenTask,
  onChanged,
}: {
  chatJID: string
  messageID: string
  defaultTitle: string
  onOpenTask: (id: number) => void
  onChanged: () => void
}) {
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<'menu' | 'add'>('menu')
  const [tasks, setTasks] = useState<Task[]>([])
  const [q, setQ] = useState('')
  const [role, setRole] = useState('comment')
  const [busy, setBusy] = useState(false)

  function close() {
    setOpen(false)
    setMode('menu')
    setQ('')
  }

  async function newTask() {
    setBusy(true)
    try {
      const t = await api.createTask({
        title: (defaultTitle || 'Task').slice(0, 80),
        origin_chat_jid: chatJID,
        origin_message_id: messageID,
      })
      close()
      onChanged()
      onOpenTask(t.id)
    } finally {
      setBusy(false)
    }
  }

  async function openAdd() {
    setMode('add')
    const t = await api.tasks({}).catch(() => [])
    setTasks(t)
  }

  async function linkTo(taskId: number) {
    setBusy(true)
    try {
      await api.linkTaskMessage(taskId, { chat_jid: chatJID, message_id: messageID, role })
      close()
      onChanged()
    } finally {
      setBusy(false)
    }
  }

  const shown = tasks
    .filter((t) => !q.trim() || t.title.toLowerCase().includes(q.trim().toLowerCase()))
    .slice(0, 20)

  return (
    <span className="relative">
      <button
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
        title="Task"
        className="rounded px-1 text-xs text-neutral-500 opacity-0 transition hover:text-emerald-300 group-hover:opacity-100"
      >
        ✓
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={close} />
          <div
            className="absolute right-0 z-40 mt-1 w-60 rounded-lg border border-neutral-700 bg-neutral-900 p-2 text-sm shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            {mode === 'menu' ? (
              <div className="flex flex-col">
                <button
                  onClick={newTask}
                  disabled={busy}
                  className="rounded-md px-2 py-1.5 text-left hover:bg-neutral-800 disabled:opacity-50"
                >
                  ＋ New task from this
                </button>
                <button
                  onClick={openAdd}
                  className="rounded-md px-2 py-1.5 text-left hover:bg-neutral-800"
                >
                  ↳ Add to existing task…
                </button>
              </div>
            ) : (
              <div>
                <div className="mb-2 flex gap-1 text-[11px]">
                  {LINK_ROLES.map((r) => (
                    <button
                      key={r}
                      onClick={() => setRole(r)}
                      className={
                        'rounded-full px-2 py-0.5 capitalize transition ' +
                        (role === r ? 'bg-emerald-500 text-neutral-950' : 'bg-neutral-800 text-neutral-300')
                      }
                    >
                      {r}
                    </button>
                  ))}
                </div>
                <input
                  autoFocus
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                  placeholder="Search tasks"
                  className="mb-2 w-full rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm outline-none"
                />
                <div className="max-h-48 overflow-y-auto">
                  {shown.length === 0 && <div className="px-2 py-1 text-xs text-neutral-600">No tasks</div>}
                  {shown.map((t) => (
                    <button
                      key={t.id}
                      onClick={() => linkTo(t.id)}
                      disabled={busy}
                      dir="auto"
                      className="block w-full truncate rounded-md px-2 py-1 text-left hover:bg-neutral-800 disabled:opacity-50"
                    >
                      {t.title}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
        </>
      )}
    </span>
  )
}

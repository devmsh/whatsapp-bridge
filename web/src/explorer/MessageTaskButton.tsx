import { useState } from 'react'
import { api, type Task } from '../api'

const LINK_ROLES = ['comment', 'completion', 'attachment', 'related']

// MessageTaskButton is the "promote this message into a task" action on a
// bubble: create a fresh task (origin = this message) or link it to an
// existing one as a comment/completion/attachment/related event — which is
// how a task can span multiple chats.
//
// Visually it matches the rest of the gutter actions (Reply, React, Forward,
// Star, Edit): a circular 28×28 icon button that fades in on row hover. The
// `side` prop mirrors BubbleActions and aligns the popover toward the chat
// edge so it never gets clipped by the bubble.
export function MessageTaskButton({
  chatJID,
  messageID,
  defaultTitle,
  onOpenTask,
  onChanged,
  side = 'right',
}: {
  chatJID: string
  messageID: string
  defaultTitle: string
  onOpenTask: (id: number) => void
  onChanged: () => void
  side?: 'left' | 'right'
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

  // Anchor the popover to whichever gutter side this button sits on so it
  // never floats off-screen against the bubble. left-gutter button → popover
  // hangs from its left edge (toward the chat edge); right-gutter → right.
  const popoverAnchor = side === 'left' ? 'left-0' : 'right-0'

  return (
    <span className="relative">
      <button
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
        title="Promote to task"
        aria-label="Promote to task"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-300 transition hover:bg-neutral-700 hover:text-emerald-300"
      >
        {/* clipboard-with-check — reads as "task" without needing a label */}
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="6" y="3" width="12" height="4" rx="1" />
          <path d="M6 5H4a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1h-2" />
          <path d="m8 13 3 3 5-6" />
        </svg>
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={close} />
          <div
            className={
              'absolute z-40 mt-1 w-60 rounded-lg border border-neutral-700 bg-neutral-900 p-2 text-sm shadow-xl ' +
              popoverAnchor
            }
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

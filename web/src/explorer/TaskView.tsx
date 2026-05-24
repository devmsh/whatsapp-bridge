import { useEffect, useMemo, useState } from 'react'
import {
  api,
  type Circle,
  type Contact,
  type TaskDetail,
  type TaskMessageLink,
  type TaskStatus,
} from '../api'
import { clockTime, dayLabel, jidUser, mediaURL } from './format'

const STATUSES: TaskStatus[] = ['open', 'in_progress', 'done', 'cancelled']
const PRIORITIES = ['low', 'normal', 'high']
const ROLE_ORDER = ['origin', 'completion', 'comment', 'attachment', 'related']
const ROLE_LABEL: Record<string, string> = {
  origin: 'Started here',
  completion: 'Completed here',
  comment: 'Comments',
  attachment: 'Attachments',
  related: 'Related',
}

function toDateInput(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// TaskView shows and edits one task, including its cross-chat linked messages.
export function TaskView({
  taskId,
  contacts,
  circles,
  nameMap,
  version,
  onOpenChat,
  onChanged,
  onDeleted,
}: {
  taskId: number
  contacts: Contact[]
  circles: Circle[]
  nameMap: Map<string, string>
  version: number
  onOpenChat: (jid: string) => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const [detail, setDetail] = useState<TaskDetail | null>(null)
  const [titleDraft, setTitleDraft] = useState('')
  const [editingTitle, setEditingTitle] = useState(false)
  const [assigning, setAssigning] = useState(false)
  const [addingCircle, setAddingCircle] = useState(false)

  function reload() {
    return api.getTask(taskId).then(setDetail)
  }
  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, version])

  const task = detail?.task
  const msgs = detail?.messages || []
  const grouped = useMemo(() => {
    const g: Record<string, TaskMessageLink[]> = {}
    for (const m of msgs) (g[m.role] ||= []).push(m)
    return g
  }, [msgs])

  async function patch(body: Parameters<typeof api.updateTask>[1]) {
    await api.updateTask(taskId, body)
    await reload()
    onChanged()
  }

  async function unlink(m: TaskMessageLink) {
    await api.unlinkTaskMessage(taskId, { chat_jid: m.chat_jid, message_id: m.message_id, role: m.role })
    await reload()
    onChanged()
  }

  async function removeCircle(cid: number) {
    await api.removeTaskCircle(taskId, cid)
    await reload()
    onChanged()
  }

  async function addCircle(cid: number) {
    await api.addTaskCircle(taskId, cid)
    setAddingCircle(false)
    await reload()
    onChanged()
  }

  async function del() {
    if (!task) return
    if (!confirm(`Delete task "${task.title}"?`)) return
    await api.deleteTask(taskId)
    onChanged()
    onDeleted()
  }

  if (!task) {
    return <div className="flex h-full items-center justify-center text-sm text-neutral-600">Loading…</div>
  }

  const assigneeName = task.assignee_jid
    ? nameMap.get(task.assignee_jid) || '+' + jidUser(task.assignee_jid)
    : ''
  const taskCircles = circles.filter((c) => (task.circle_ids || []).includes(c.id))

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-start gap-3 border-b border-neutral-800 px-5 py-3">
        <div className="min-w-0 flex-1">
          {editingTitle ? (
            <input
              autoFocus
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              onBlur={() => {
                setEditingTitle(false)
                if (titleDraft.trim() && titleDraft !== task.title) patch({ title: titleDraft.trim() })
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
              }}
              className="w-full rounded-md border border-neutral-700 bg-neutral-900 px-2 py-1 text-base font-semibold outline-none"
            />
          ) : (
            <button
              onClick={() => {
                setTitleDraft(task.title)
                setEditingTitle(true)
              }}
              dir="auto"
              className="text-start text-base font-semibold hover:text-emerald-300"
            >
              {task.title}
            </button>
          )}
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-neutral-500">
            <select
              value={task.status}
              onChange={(e) => patch({ status: e.target.value as TaskStatus })}
              className="rounded border border-neutral-700 bg-neutral-900 px-1.5 py-0.5 text-xs"
            >
              {STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
            <select
              value={task.priority}
              onChange={(e) => patch({ priority: e.target.value })}
              className="rounded border border-neutral-700 bg-neutral-900 px-1.5 py-0.5 text-xs"
            >
              {PRIORITIES.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
            <input
              type="date"
              value={toDateInput(task.due_at)}
              onChange={(e) => {
                const v = e.target.value
                patch({ due_at: v ? Math.floor(new Date(v).getTime() / 1000) : 0 })
              }}
              className="rounded border border-neutral-700 bg-neutral-900 px-1.5 py-0.5 text-xs"
            />
          </div>
        </div>
        <button
          onClick={del}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
          title="Delete task"
        >
          🗑
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {/* assignee + circles */}
        <div className="mb-4 flex flex-wrap items-center gap-2 text-xs">
          <span className="text-neutral-500">Assignee:</span>
          <span className="relative">
            <button
              onClick={() => setAssigning((a) => !a)}
              className="rounded-full bg-neutral-800 px-2 py-0.5 text-neutral-200 hover:bg-neutral-700"
            >
              {assigneeName || 'Unassigned'} ▾
            </button>
            {assigning && (
              <AssigneePicker
                contacts={contacts}
                nameMap={nameMap}
                onPick={async (jid) => {
                  setAssigning(false)
                  await patch({ assignee_jid: jid })
                }}
                onClose={() => setAssigning(false)}
              />
            )}
          </span>

          <span className="ml-3 text-neutral-500">Circles:</span>
          {taskCircles.map((c) => (
            <span
              key={c.id}
              className="inline-flex items-center gap-1 rounded-full px-2 py-0.5"
              style={{ backgroundColor: (c.color || '#64748b') + '33', color: c.color || '#cbd5e1' }}
            >
              {c.name}
              <button onClick={() => removeCircle(c.id)} className="opacity-70 hover:text-red-300">
                ✕
              </button>
            </span>
          ))}
          <span className="relative">
            <button
              onClick={() => setAddingCircle((a) => !a)}
              className="rounded-full border border-neutral-700 px-2 py-0.5 text-neutral-400 hover:bg-neutral-800"
            >
              + circle
            </button>
            {addingCircle && (
              <div className="absolute left-0 top-7 z-20 max-h-56 w-48 overflow-y-auto rounded-lg border border-neutral-700 bg-neutral-900 p-1 shadow-xl">
                {circles
                  .filter((c) => !(task.circle_ids || []).includes(c.id))
                  .map((c) => (
                    <button
                      key={c.id}
                      onClick={() => addCircle(c.id)}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-neutral-800"
                    >
                      <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: c.color || '#64748b' }} />
                      <span className="truncate">{c.name}</span>
                    </button>
                  ))}
              </div>
            )}
          </span>
        </div>

        {/* description */}
        <textarea
          defaultValue={task.description}
          onBlur={(e) => {
            if (e.target.value !== task.description) patch({ description: e.target.value })
          }}
          dir="auto"
          rows={2}
          placeholder="Add notes…"
          className="mb-5 w-full resize-none rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-600"
        />

        {/* linked messages by role */}
        {msgs.length === 0 && (
          <div className="text-xs text-neutral-600">
            No linked messages yet. Open a chat and use the ✓ button on a message to link it as the
            origin, completion, a comment, or an attachment — even across different chats.
          </div>
        )}
        {ROLE_ORDER.filter((r) => grouped[r]?.length).map((role) => (
          <div key={role} className="mb-4">
            <div className="mb-1 text-[11px] uppercase tracking-wide text-neutral-500">
              {ROLE_LABEL[role] || role}
            </div>
            <div className="space-y-1.5">
              {grouped[role].map((m) => (
                <LinkedMessage key={role + m.chat_jid + m.message_id} m={m} nameMap={nameMap} onOpen={() => onOpenChat(m.chat_jid)} onUnlink={() => unlink(m)} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function LinkedMessage({
  m,
  nameMap,
  onOpen,
  onUnlink,
}: {
  m: TaskMessageLink
  nameMap: Map<string, string>
  onOpen: () => void
  onUnlink: () => void
}) {
  const who = m.sender ? nameMap.get(m.sender) || m.sender_name || m.push_name || '+' + jidUser(m.sender) : ''
  const url = mediaURL(m.media_path)
  return (
    <div className="group flex items-start gap-3 rounded-lg border border-neutral-800 bg-neutral-900/50 p-2">
      <button onClick={onOpen} className="min-w-0 flex-1 text-left">
        <div className="flex items-center gap-2 text-[11px] text-neutral-500">
          {who && <span className="truncate text-neutral-400">{who}</span>}
          {m.timestamp ? (
            <span>
              {dayLabel(m.timestamp)} {clockTime(m.timestamp)}
            </span>
          ) : (
            <span>linked</span>
          )}
        </div>
        {m.media_type === 'image' && url ? (
          <img src={url} alt="" className="mt-1 max-h-40 rounded" />
        ) : m.media_type ? (
          <div className="mt-1 text-sm text-neutral-300">📎 {m.media_type}</div>
        ) : null}
        {m.content && (
          <div dir="auto" className="mt-0.5 whitespace-pre-wrap break-words text-sm">
            {m.content}
          </div>
        )}
        {!m.content && !m.media_type && !m.message_id && (
          <div className="mt-0.5 text-sm text-neutral-500">(whole chat)</div>
        )}
      </button>
      <button
        onClick={onUnlink}
        className="shrink-0 rounded px-1.5 py-0.5 text-xs text-neutral-500 opacity-0 transition hover:text-red-400 group-hover:opacity-100"
        title="Unlink"
      >
        ✕
      </button>
    </div>
  )
}

function AssigneePicker({
  contacts,
  nameMap,
  onPick,
  onClose,
}: {
  contacts: Contact[]
  nameMap: Map<string, string>
  onPick: (jid: string) => void
  onClose: () => void
}) {
  const [q, setQ] = useState('')
  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (!needle) return []
    return contacts
      .map((c) => ({ jid: c.jid, label: nameMap.get(c.jid) || c.name || c.push_name || '+' + (c.phone || jidUser(c.jid)) }))
      .filter((r) => r.label.toLowerCase().includes(needle))
      .slice(0, 30)
  }, [q, contacts, nameMap])
  return (
    <>
      <div className="fixed inset-0 z-30" onClick={onClose} />
      <div className="absolute left-0 top-7 z-40 w-60 rounded-lg border border-neutral-700 bg-neutral-900 p-2 shadow-xl">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search contacts"
          className="mb-2 w-full rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm outline-none"
        />
        <button
          onClick={() => onPick('')}
          className="mb-1 w-full rounded-md px-2 py-1 text-left text-xs text-neutral-500 hover:bg-neutral-800"
        >
          Clear assignee
        </button>
        <div className="max-h-48 overflow-y-auto">
          {rows.map((r) => (
            <button
              key={r.jid}
              onClick={() => onPick(r.jid)}
              className="block w-full truncate rounded-md px-2 py-1 text-left text-sm hover:bg-neutral-800"
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
    </>
  )
}

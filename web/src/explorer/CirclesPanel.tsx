import { useState } from 'react'
import { api, type Circle } from '../api'
import { pickColor } from './colors'

const DND_TYPE = 'application/x-circle'

// CirclesPanel is the sidebar tree of circles plus a create box. Circles can be
// dragged onto each other to nest them; nested circles show indented under their
// parent.
export function CirclesPanel({
  circles,
  selected,
  recoActive,
  onOpen,
  onOpenReco,
  onCreated,
  onChanged,
  onFocusCircle,
}: {
  circles: Circle[]
  selected: number | null
  recoActive: boolean
  onOpen: (id: number) => void
  onOpenReco: () => void
  onCreated: (c: Circle) => void
  onChanged: () => void
  onFocusCircle: (id: number) => void
}) {
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [dragId, setDragId] = useState<number | null>(null)
  const [overId, setOverId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set())
  const [creatingUnder, setCreatingUnder] = useState<number | null>(null)
  const [subName, setSubName] = useState('')
  const [subBusy, setSubBusy] = useState(false)

  async function create() {
    const n = name.trim()
    if (!n || busy) return
    setBusy(true)
    try {
      const c = await api.createCircle(n, pickColor(circles))
      setName('')
      setCreating(false)
      onCreated(c)
    } finally {
      setBusy(false)
    }
  }

  async function nest(childId: number, parentId: number) {
    if (childId === parentId) return
    try {
      await api.addCircleMember(parentId, 'circle', String(childId))
      onChanged()
    } catch (e) {
      setError((e as Error).message)
      setTimeout(() => setError(''), 3500)
    }
  }

  async function createSub(parentId: number) {
    const n = subName.trim()
    if (!n || subBusy) return
    setSubBusy(true)
    try {
      const sub = await api.createCircle(n, pickColor(circles))
      await api.addCircleMember(parentId, 'circle', String(sub.id))
      setSubName('')
      setCreatingUnder(null)
      onChanged()
    } catch (e) {
      setError((e as Error).message)
      setTimeout(() => setError(''), 3500)
    } finally {
      setSubBusy(false)
    }
  }

  const byId = new Map(circles.map((c) => [c.id, c]))
  const isChild = new Set<number>()
  for (const c of circles) for (const id of c.child_circles || []) isChild.add(id)
  const roots = circles.filter((c) => !isChild.has(c.id))

  function toggle(id: number) {
    setCollapsed((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  }

  function renderNode(c: Circle, depth: number, path: Set<number>) {
    const children = (c.child_circles || [])
      .map((id) => byId.get(id))
      .filter((x): x is Circle => !!x && !path.has(x.id))
    const hasChildren = children.length > 0
    const open = !collapsed.has(c.id)
    return (
      <div key={depth + '-' + c.id}>
        <div
          draggable
          onClick={() => onOpen(c.id)}
          onDragStart={(e) => {
            e.dataTransfer.setData(DND_TYPE, String(c.id))
            e.dataTransfer.effectAllowed = 'move'
            setDragId(c.id)
          }}
          onDragEnd={() => {
            setDragId(null)
            setOverId(null)
          }}
          onDragOver={(e) => {
            if (dragId != null && dragId !== c.id) {
              e.preventDefault()
              setOverId(c.id)
            }
          }}
          onDragLeave={() => setOverId((id) => (id === c.id ? null : id))}
          onDrop={(e) => {
            e.preventDefault()
            const childId = Number(e.dataTransfer.getData(DND_TYPE))
            setOverId(null)
            setDragId(null)
            if (childId) nest(childId, c.id)
          }}
          title="Drag onto another circle to nest it"
          style={{ paddingLeft: 8 + depth * 16 }}
          className={
            'group flex w-full cursor-pointer items-center gap-2 py-2.5 pr-2 text-left transition ' +
            (overId === c.id
              ? 'bg-emerald-500/10 ring-2 ring-inset ring-emerald-500'
              : selected === c.id
                ? 'bg-neutral-800'
                : 'hover:bg-neutral-900') +
            (dragId === c.id ? ' opacity-50' : '')
          }
        >
          {hasChildren ? (
            <button
              onClick={(e) => {
                e.stopPropagation()
                toggle(c.id)
              }}
              className="w-4 shrink-0 text-neutral-500 hover:text-neutral-200"
            >
              {open ? '▾' : '▸'}
            </button>
          ) : (
            <span className="w-4 shrink-0" />
          )}
          <span
            className="h-3 w-3 shrink-0 rounded-full"
            style={{ backgroundColor: c.color || '#737373' }}
          />
          <span dir="auto" className="min-w-0 flex-1 truncate text-sm font-medium">
            {c.name}
          </span>
          <button
            onClick={(e) => {
              e.stopPropagation()
              setSubName('')
              setCreatingUnder(c.id)
              setCollapsed((s) => {
                const n = new Set(s)
                n.delete(c.id)
                return n
              })
            }}
            title="New sub-circle"
            className="w-5 shrink-0 text-neutral-500 opacity-0 transition hover:text-emerald-300 group-hover:opacity-100"
          >
            +
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              onFocusCircle(c.id)
            }}
            title="Focus on this circle"
            className="shrink-0 rounded px-1.5 py-0.5 text-[11px] text-neutral-500 opacity-0 transition hover:text-emerald-300 group-hover:opacity-100"
          >
            Focus
          </button>
          <span className="w-5 shrink-0 text-center text-[11px] text-neutral-500">{c.member_count}</span>
        </div>
        {creatingUnder === c.id && (
          <div className="flex gap-2 py-1.5 pr-2" style={{ paddingLeft: 8 + (depth + 1) * 16 }}>
            <input
              autoFocus
              value={subName}
              onChange={(e) => setSubName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') createSub(c.id)
                if (e.key === 'Escape') {
                  setCreatingUnder(null)
                  setSubName('')
                }
              }}
              placeholder="Sub-circle name"
              className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={() => createSub(c.id)}
              disabled={subBusy}
              className="rounded-lg bg-emerald-500 px-2 text-xs font-medium text-neutral-950 disabled:opacity-50"
            >
              Add
            </button>
          </div>
        )}
        {hasChildren &&
          open &&
          children.map((ch) => renderNode(ch, depth + 1, new Set([...path, c.id])))}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <button
        onClick={onOpenReco}
        className={
          'mx-2 mt-2 flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition ' +
          (recoActive
            ? 'bg-emerald-500/15 text-emerald-300'
            : 'bg-neutral-800/60 text-neutral-200 hover:bg-neutral-800')
        }
      >
        ✨ Recommendations
      </button>

      <div className="p-2">
        {creating ? (
          <div className="flex gap-2">
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') create()
                if (e.key === 'Escape') setCreating(false)
              }}
              placeholder="Circle name"
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
            + New circle
          </button>
        )}
      </div>

      {error && (
        <div className="mx-2 mb-1 rounded-lg bg-red-500/15 px-3 py-1.5 text-xs text-red-300">{error}</div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {circles.length === 0 && (
          <div className="p-4 text-center text-xs text-neutral-600">
            No circles yet. Create one to group chats and contacts.
          </div>
        )}
        {roots.map((c) => renderNode(c, 0, new Set()))}
      </div>
    </div>
  )
}

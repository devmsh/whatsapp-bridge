import { useEffect, useMemo, useState } from 'react'
import { api, type Circle, type UnassignedChat } from '../api'
import { chatListTime } from './format'
import { flattenCircleTree, type CircleOption } from './circleTree'

// Row and Section live at module level on purpose. Declared inside
// UnassignedView they would be a new component type on every render, so React
// would unmount and remount every row — dropping focus and resetting the open
// <select> mid-interaction.
function Row({
  row,
  picked,
  busy,
  options,
  onToggle,
  onAssign,
}: {
  row: UnassignedChat
  picked: boolean
  busy: boolean
  options: CircleOption[]
  onToggle: (jid: string) => void
  onAssign: (row: UnassignedChat, circleId: number) => void
}) {
  return (
    <div className="flex items-center gap-3 border-b border-neutral-900 px-4 py-2.5 hover:bg-neutral-900/60">
      <input
        type="checkbox"
        checked={picked}
        onChange={() => onToggle(row.jid)}
        className="h-4 w-4 shrink-0 accent-emerald-500"
      />
      <div className="min-w-0 flex-1">
        <div dir="auto" className="truncate text-sm font-medium">
          {row.name}
        </div>
        <div className="text-xs text-neutral-500">
          {row.recent_count > 0 ? (
            <span className="text-emerald-600">{row.recent_count} in 30 days</span>
          ) : (
            <span>quiet this month</span>
          )}
          {' · '}
          {row.message_count} total
          {row.kind === 'group' && row.participants > 0 ? ` · ${row.participants} people` : ''}
          {row.last_message_at ? ` · ${chatListTime(row.last_message_at)}` : ''}
        </div>
      </div>
      <select
        defaultValue=""
        disabled={busy}
        onChange={(e) => {
          const id = Number(e.target.value)
          e.target.value = ''
          onAssign(row, id)
        }}
        className="shrink-0 rounded-lg border border-neutral-700 bg-neutral-950 px-2 py-1 text-xs outline-none focus:border-neutral-500"
      >
        <option value="">Add to circle…</option>
        {options.map((c) => (
          <option key={c.id} value={c.id}>
            {c.label}
          </option>
        ))}
      </select>
    </div>
  )
}

function Section({
  title,
  rows,
  picked,
  busy,
  options,
  onToggle,
  onAssign,
}: {
  title: string
  rows: UnassignedChat[]
  picked: Set<string>
  busy: boolean
  options: CircleOption[]
  onToggle: (jid: string) => void
  onAssign: (row: UnassignedChat, circleId: number) => void
}) {
  if (rows.length === 0) return null
  return (
    <>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-neutral-800 bg-neutral-950/95 px-4 py-1.5 text-xs font-semibold text-neutral-400 backdrop-blur">
        {title}
        <span className="text-neutral-600">{rows.length}</span>
      </div>
      {rows.map((r) => (
        <Row
          key={r.jid}
          row={r}
          picked={picked.has(r.jid)}
          busy={busy}
          options={options}
          onToggle={onToggle}
          onAssign={onAssign}
        />
      ))}
    </>
  )
}

// UnassignedView is the virtual "Unassigned" circle: every live group and every
// person you talk to that sits in no circle yet. It is a to-do list, not a
// store — a row disappears the moment you file it. Chats you left, archived,
// deleted and hidden ones never appear, so the list only ever holds work that
// still needs a decision.
//
// Both lists are ranked by messages in the last 30 days, so what is busy right
// now sits at the top and long-dead chats sink, however big they once were.
export function UnassignedView({
  circles,
  onChanged,
  onOpenCircle,
  onOpenReco,
}: {
  circles: Circle[]
  onChanged: () => void
  onOpenCircle: (id: number) => void
  onOpenReco: () => void
}) {
  const [groups, setGroups] = useState<UnassignedChat[]>([])
  const [people, setPeople] = useState<UnassignedChat[]>([])
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState('')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [bulkCircle, setBulkCircle] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  function load() {
    setLoading(true)
    api
      .unassignedGroups()
      .then((r) => {
        setGroups(r.groups || [])
        setPeople(r.people || [])
      })
      .catch(() => {
        setGroups([])
        setPeople([])
      })
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  // The picker mirrors the sidebar tree: a sub-circle shows indented under its
  // parent, so you can tell "Sentra under ID8" from a top-level circle while
  // filing. A flat A–Z list hid that.
  const options = useMemo(() => flattenCircleTree(circles), [circles])

  const q = query.trim().toLowerCase()
  const match = (rows: UnassignedChat[]) =>
    q ? rows.filter((r) => r.name.toLowerCase().includes(q)) : rows
  const shownGroups = useMemo(() => match(groups), [groups, q])
  const shownPeople = useMemo(() => match(people), [people, q])
  const shown = [...shownGroups, ...shownPeople]
  const total = groups.length + people.length

  function fail(e: unknown) {
    setError((e as Error).message || 'Could not assign')
    setTimeout(() => setError(''), 4000)
  }

  function drop(jids: string[]) {
    const gone = new Set(jids)
    setGroups((gs) => gs.filter((g) => !gone.has(g.jid)))
    setPeople((ps) => ps.filter((p) => !gone.has(p.jid)))
    setPicked((p) => {
      const n = new Set(p)
      for (const j of jids) n.delete(j)
      return n
    })
  }

  // assign files one row and drops it from the list straight away, so what is
  // left on screen is always what still needs a decision.
  async function assign(row: UnassignedChat, circleId: number) {
    if (!circleId) return
    setBusy(true)
    try {
      await api.addCircleMember(circleId, row.kind, row.jid)
      drop([row.jid])
      onChanged()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  async function assignPicked() {
    const circleId = Number(bulkCircle)
    if (!circleId || picked.size === 0 || busy) return
    setBusy(true)
    const byJID = new Map(shown.concat(groups, people).map((r) => [r.jid, r]))
    const done: string[] = []
    try {
      for (const jid of picked) {
        const row = byJID.get(jid)
        if (!row) continue
        await api.addCircleMember(circleId, row.kind, row.jid)
        done.push(jid)
      }
    } catch (e) {
      fail(e)
    } finally {
      drop(done)
      setBusy(false)
      if (done.length) onChanged()
    }
  }

  function toggle(jid: string) {
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(jid)) n.delete(jid)
      else n.add(jid)
      return n
    })
  }

  const allShownPicked = shown.length > 0 && shown.every((r) => picked.has(r.jid))

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between gap-3 border-b border-neutral-800 px-4 py-3">
        <div className="min-w-0">
          <div className="text-sm font-semibold">📥 Unassigned</div>
          <div className="truncate text-xs text-neutral-500">
            {loading
              ? 'Loading…'
              : `${groups.length} group${groups.length === 1 ? '' : 's'} and ${people.length} ` +
                `${people.length === 1 ? 'person' : 'people'} with no circle · busiest first`}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            onClick={onOpenReco}
            title="These chats are what Recommendations analyses"
            className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            ✨ Suggest circles
          </button>
          <button
            onClick={load}
            className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Refresh
          </button>
        </div>
      </header>

      <div className="flex items-center gap-2 border-b border-neutral-800 px-4 py-2">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter groups and people…"
          className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-1.5 text-sm outline-none focus:border-neutral-500"
        />
        {shown.length > 0 && (
          <button
            onClick={() =>
              setPicked((p) => {
                const n = new Set(p)
                for (const r of shown) {
                  if (allShownPicked) n.delete(r.jid)
                  else n.add(r.jid)
                }
                return n
              })
            }
            className="shrink-0 rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            {allShownPicked ? 'Clear' : 'Select all'}
          </button>
        )}
      </div>

      {error && (
        <div className="mx-4 mt-2 rounded-lg bg-red-500/15 px-3 py-1.5 text-xs text-red-300">{error}</div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading && <div className="py-10 text-center text-sm text-neutral-600">Loading…</div>}
        {!loading && total === 0 && (
          <div className="px-6 py-10 text-center text-sm text-neutral-600">
            Everything live is in a circle. Nothing to sort.
          </div>
        )}
        {!loading && total > 0 && shown.length === 0 && (
          <div className="px-6 py-10 text-center text-sm text-neutral-600">Nothing matches that filter.</div>
        )}
        <Section
          title="Groups"
          rows={shownGroups}
          picked={picked}
          busy={busy}
          options={options}
          onToggle={toggle}
          onAssign={assign}
        />
        <Section
          title="People"
          rows={shownPeople}
          picked={picked}
          busy={busy}
          options={options}
          onToggle={toggle}
          onAssign={assign}
        />
      </div>

      {picked.size > 0 && (
        <div className="flex items-center gap-2 border-t border-neutral-800 bg-neutral-900 px-4 py-3">
          <span className="shrink-0 text-sm text-neutral-300">{picked.size} selected</span>
          <select
            value={bulkCircle}
            onChange={(e) => setBulkCircle(e.target.value)}
            className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          >
            <option value="">Choose a circle…</option>
            {options.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
          <button
            onClick={assignPicked}
            disabled={!bulkCircle || busy}
            className="shrink-0 rounded-lg bg-emerald-500 px-3 py-1.5 text-sm font-medium text-neutral-950 disabled:opacity-50"
          >
            {busy ? 'Adding…' : 'Add'}
          </button>
          {bulkCircle && (
            <button
              onClick={() => onOpenCircle(Number(bulkCircle))}
              className="shrink-0 rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-300 hover:bg-neutral-800"
            >
              Open
            </button>
          )}
        </div>
      )}
    </div>
  )
}

import { useMemo, useState } from 'react'
import { api, type Tag } from '../api'
import { CIRCLE_COLORS } from './colors'

// TagChips renders a contact's tags as small colored chips.
export function TagChips({ tags }: { tags: Tag[] }) {
  if (!tags || tags.length === 0) return null
  return (
    <span className="flex flex-wrap gap-1">
      {tags.map((t) => (
        <span
          key={t.id}
          className="rounded px-1.5 py-0.5 text-[10px] font-medium"
          style={{
            backgroundColor: (t.color || '#64748b') + '33',
            color: t.color || '#cbd5e1',
          }}
        >
          {t.name}
        </span>
      ))}
    </span>
  )
}

// TagEditor is a small popover button to add/remove tags on a contact. It can
// pick from existing tags or create a new one. Calls onChanged after edits.
export function TagEditor({
  jid,
  tags,
  allTags,
  onChanged,
}: {
  jid: string
  tags: Tag[]
  allTags: Tag[]
  onChanged: () => void
}) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)

  const assigned = useMemo(() => new Set(tags.map((t) => t.id)), [tags])
  const matches = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return allTags.filter((t) => !needle || t.name.toLowerCase().includes(needle))
  }, [allTags, q])
  const canCreate =
    q.trim() && !allTags.some((t) => t.name.toLowerCase() === q.trim().toLowerCase())

  async function toggle(t: Tag) {
    setBusy(true)
    try {
      if (assigned.has(t.id)) await api.unassignTag(jid, t.id)
      else await api.assignTag(jid, { tag_id: t.id })
      onChanged()
    } finally {
      setBusy(false)
    }
  }

  async function create() {
    const name = q.trim()
    if (!name) return
    setBusy(true)
    try {
      const color = CIRCLE_COLORS[Math.floor(Math.random() * CIRCLE_COLORS.length)]
      await api.assignTag(jid, { name, color })
      setQ('')
      onChanged()
    } finally {
      setBusy(false)
    }
  }

  return (
    <span className="relative">
      <button
        onClick={(e) => {
          e.stopPropagation()
          setOpen((o) => !o)
        }}
        title="Edit tags"
        className="rounded px-1 text-xs text-neutral-500 hover:text-neutral-200"
      >
        🏷
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div
            className="absolute right-0 z-40 mt-1 w-56 rounded-lg border border-neutral-700 bg-neutral-900 p-2 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <input
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && canCreate) create()
              }}
              placeholder="Find or create tag"
              className="mb-2 w-full rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm outline-none focus:border-neutral-500"
            />
            <div className="max-h-48 overflow-y-auto">
              {matches.map((t) => (
                <button
                  key={t.id}
                  onClick={() => toggle(t)}
                  disabled={busy}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-neutral-800 disabled:opacity-50"
                >
                  <span
                    className="h-2.5 w-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: t.color || '#64748b' }}
                  />
                  <span className="min-w-0 flex-1 truncate">{t.name}</span>
                  {assigned.has(t.id) && <span className="text-emerald-400">✓</span>}
                </button>
              ))}
              {canCreate && (
                <button
                  onClick={create}
                  disabled={busy}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-emerald-400 hover:bg-neutral-800"
                >
                  + Create “{q.trim()}”
                </button>
              )}
              {matches.length === 0 && !canCreate && (
                <div className="px-2 py-1 text-xs text-neutral-600">No tags</div>
              )}
            </div>
          </div>
        </>
      )}
    </span>
  )
}

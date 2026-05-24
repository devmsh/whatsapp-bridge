import { useEffect, useState } from 'react'
import { api, type Circle, type MemberType } from '../api'
import { isGroup } from './format'

// ChatCircles is the chat-header control to add/remove the open chat to circles.
export function ChatCircles({
  jid,
  circles,
  onChanged,
}: {
  jid: string
  circles: Circle[]
  onChanged: () => void
}) {
  const type: MemberType = isGroup(jid) ? 'group' : 'contact'
  const [memberIds, setMemberIds] = useState<Set<number>>(new Set())
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)

  useEffect(() => {
    let cancel = false
    setOpen(false)
    api
      .circlesForMember(type, jid)
      .then((cs) => {
        if (!cancel) setMemberIds(new Set((cs || []).map((c) => c.id)))
      })
      .catch(() => {})
    return () => {
      cancel = true
    }
  }, [jid, type])

  async function toggle(c: Circle) {
    setBusy(c.id)
    try {
      if (memberIds.has(c.id)) {
        await api.removeCircleMember(c.id, type, jid)
        setMemberIds((s) => {
          const n = new Set(s)
          n.delete(c.id)
          return n
        })
      } else {
        await api.addCircleMember(c.id, type, jid)
        setMemberIds((s) => new Set(s).add(c.id))
      }
      onChanged()
    } finally {
      setBusy(null)
    }
  }

  const mine = circles.filter((c) => memberIds.has(c.id))

  return (
    <div className="relative shrink-0">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-lg border border-neutral-700 px-2.5 py-1.5 text-xs text-neutral-300 transition hover:bg-neutral-800"
        title="Add this chat to circles"
      >
        {mine.length > 0 ? (
          <>
            <span className="flex -space-x-1">
              {mine.slice(0, 3).map((c) => (
                <span
                  key={c.id}
                  className="h-3 w-3 rounded-full ring-1 ring-neutral-900"
                  style={{ backgroundColor: c.color || '#737373' }}
                />
              ))}
            </span>
            <span>
              {mine.length} circle{mine.length > 1 ? 's' : ''}
            </span>
          </>
        ) : (
          <span>＋ Add to circle</span>
        )}
        <span className="text-neutral-500">▾</span>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-9 z-20 max-h-72 w-60 overflow-y-auto rounded-lg border border-neutral-700 bg-neutral-900 p-1 shadow-xl">
            {circles.length === 0 && (
              <div className="p-3 text-center text-xs text-neutral-500">
                No circles yet. Create one in the Circles tab.
              </div>
            )}
            {circles.map((c) => (
              <button
                key={c.id}
                onClick={() => toggle(c)}
                disabled={busy === c.id}
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition hover:bg-neutral-800 disabled:opacity-50"
              >
                <span
                  className="h-3 w-3 shrink-0 rounded-full"
                  style={{ backgroundColor: c.color || '#737373' }}
                />
                <span dir="auto" className="min-w-0 flex-1 truncate">
                  {c.name}
                </span>
                <span className="w-4 shrink-0 text-center text-emerald-400">
                  {memberIds.has(c.id) ? '✓' : ''}
                </span>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

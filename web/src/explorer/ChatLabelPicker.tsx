import { useState } from 'react'
import { useChatLabels, LABEL_PALETTE } from '../hooks/useChatLabels'

// ChatLabelPicker — popover under the header label button. Toggle which
// labels are on this chat, create a new one inline (name + colour), or
// delete a label entirely. All writes go through the localStorage-backed
// useChatLabels hook, so the chat-list dots update live.
export function ChatLabelPicker({ jid, onClose }: { jid: string; onClose: () => void }) {
  const { labels, assignments, toggle, addLabel, removeLabel } = useChatLabels()
  const assigned = new Set(assignments[jid] || [])
  const [name, setName] = useState('')
  const [color, setColor] = useState(LABEL_PALETTE[0])

  const create = () => {
    if (!name.trim()) return
    addLabel(name, color)
    setName('')
  }

  return (
    <>
      {/* Click-away backdrop. */}
      <div className="fixed inset-0 z-30" onClick={onClose} />
      <div className="absolute right-0 top-full z-40 mt-1 w-64 overflow-hidden rounded-xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60">
        <div className="border-b border-neutral-800 px-3 py-2 text-xs font-semibold text-neutral-300">
          Labels
        </div>
        <ul className="max-h-56 overflow-y-auto py-1">
          {labels.map((l) => {
            const on = assigned.has(l.id)
            return (
              <li key={l.id} className="group flex items-center gap-2 px-3 py-1.5 hover:bg-neutral-800">
                <button
                  onClick={() => toggle(jid, l.id)}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left"
                >
                  <span
                    className="h-3 w-3 shrink-0 rounded-full"
                    style={{ backgroundColor: l.color }}
                    aria-hidden="true"
                  />
                  <span className="truncate text-sm text-neutral-200">{l.name}</span>
                  {on && (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" className="ml-auto shrink-0 text-emerald-400" aria-label="Assigned">
                      <path d="m20 6-11 11L4 12" />
                    </svg>
                  )}
                </button>
                <button
                  onClick={() => removeLabel(l.id)}
                  title="Delete label"
                  aria-label={`Delete label ${l.name}`}
                  className="shrink-0 rounded p-1 text-neutral-600 opacity-0 transition hover:bg-red-500/15 hover:text-red-300 group-hover:opacity-100"
                >
                  <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M6 6l1 14a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-14" />
                  </svg>
                </button>
              </li>
            )
          })}
          {labels.length === 0 && (
            <li className="px-3 py-2 text-center text-xs text-neutral-500">No labels yet.</li>
          )}
        </ul>

        {/* Inline create. */}
        <div className="space-y-2 border-t border-neutral-800 px-3 py-2">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') create()
            }}
            placeholder="New label…"
            className="w-full rounded-lg border border-neutral-700 bg-neutral-950 px-2.5 py-1.5 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              {LABEL_PALETTE.map((c) => (
                <button
                  key={c}
                  onClick={() => setColor(c)}
                  aria-label={`Pick colour ${c}`}
                  className={
                    'h-4 w-4 rounded-full transition ' +
                    (color === c ? 'ring-2 ring-white ring-offset-1 ring-offset-neutral-900' : '')
                  }
                  style={{ backgroundColor: c }}
                />
              ))}
            </div>
          </div>
          <button
            onClick={create}
            disabled={!name.trim()}
            className="w-full rounded-lg bg-emerald-600 px-2.5 py-1.5 text-xs font-medium text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
          >
            Add label
          </button>
        </div>
      </div>
    </>
  )
}

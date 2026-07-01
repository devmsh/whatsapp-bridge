import { useState } from 'react'
import type { Circle } from '../api'

// FocusSwitcher is a standalone, reusable circle switcher: a compact trigger
// button showing the active circle (color swatch + name) or a neutral
// "Focus" label, plus a dropdown of every circle. Clicking a row enters or
// re-targets Focus Mode via onSelect. Self-contained — does not import
// Explorer.tsx or FocusMode.tsx; those wire it in separately.
export function FocusSwitcher({
  circles,
  activeCircleId,
  onSelect,
}: {
  circles: Circle[]
  activeCircleId: number | null
  onSelect: (id: number) => void
}) {
  const [open, setOpen] = useState(false)
  const active = activeCircleId != null ? circles.find((c) => c.id === activeCircleId) : undefined

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        title="Focus mode"
        aria-label="Focus mode"
        aria-expanded={open}
        className={
          'flex items-center gap-2 rounded-lg border border-neutral-700 bg-neutral-800 px-2.5 py-1.5 text-sm text-neutral-300 transition hover:bg-neutral-700 ' +
          (open ? 'bg-neutral-700 text-neutral-100' : '')
        }
      >
        {active ? (
          <>
            <span
              className="h-2.5 w-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: active.color || '#737373' }}
            />
            <span className="max-w-[10rem] truncate">{active.name}</span>
          </>
        ) : (
          <span>Focus</span>
        )}
        <svg
          viewBox="0 0 24 24"
          width="12"
          height="12"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
          className="shrink-0"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <>
          {/* Click-away catcher dismisses on any outside click. */}
          <div
            onClick={() => setOpen(false)}
            className="fixed inset-0 z-30"
            aria-hidden="true"
          />
          <div className="absolute left-0 top-full z-40 mt-1 max-h-80 w-56 overflow-y-auto rounded-lg border border-neutral-700 bg-neutral-900 py-1 shadow-2xl shadow-black/60">
            {circles.length === 0 && (
              <div className="px-3 py-2 text-xs text-neutral-500">No circles yet.</div>
            )}
            {circles.map((c) => (
              <button
                key={c.id}
                onClick={() => {
                  onSelect(c.id)
                  setOpen(false)
                }}
                className={
                  'flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition hover:bg-neutral-800 ' +
                  (c.id === activeCircleId ? 'bg-neutral-800/70 text-neutral-100' : 'text-neutral-200')
                }
              >
                <span
                  className="h-2.5 w-2.5 shrink-0 rounded-full"
                  style={{ backgroundColor: c.color || '#737373' }}
                />
                <span dir="auto" className="min-w-0 flex-1 truncate">{c.name}</span>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

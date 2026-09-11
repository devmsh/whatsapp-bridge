import { useEffect, useRef, useState } from 'react'

// HeaderOverflow is the "⋯" menu in the thread header.
//
// It used to be a wrapping grid of bare icon buttons. Two things were wrong
// with that, and both made it unusable rather than merely ugly:
//
//   * An icon alone does not say what it does. A tag, a calendar and a
//     download sitting in a row are a guessing game, and `title` only helps
//     if you already know to hover and wait.
//   * The container closed the menu on ANY click inside it. Actions that open
//     something of their own — the label picker, the date jump, the circle
//     popover — were therefore impossible: the menu vanished and took the
//     thing you had just opened with it.
//
// So it is a real menu now: one labelled row per action. Rows that finish
// their work immediately close the menu; rows that open something of their own
// keep it open, and say so through `keepOpen`.

export interface OverflowItem {
  /** Stable key, also used as the test/debug handle. */
  id: string
  label: string
  icon: React.ReactNode
  onClick: () => void
  /** Extra line under the label, for anything that needs a word of context. */
  hint?: string
  disabled?: boolean
  /** The row reads as on — a picker it owns is currently open. */
  active?: boolean
  /** Keep the menu open: this row opens something that lives inside it. */
  keepOpen?: boolean
  /** Destructive actions are tinted, so hiding a chat never looks routine. */
  danger?: boolean
}

export function HeaderOverflow({
  items,
  footer,
  label = 'More actions',
}: {
  items: OverflowItem[]
  /** Self-contained widgets that manage their own popovers. Clicks here never
   *  close the menu. */
  footer?: React.ReactNode
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)

  // Close on outside click and on Escape. Escape stops here rather than
  // bubbling, so it does not also trigger the hidden-chat panic relock.
  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.stopPropagation()
        e.preventDefault()
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey, true)
    }
  }, [open])

  const shown = items.filter(Boolean)

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        onClick={() => setOpen((v) => !v)}
        title={label}
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="menu"
        className={
          'flex h-8 w-8 items-center justify-center rounded-full transition ' +
          (open
            ? 'bg-neutral-800 text-neutral-100'
            : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200')
        }
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor" aria-hidden="true">
          <circle cx="12" cy="5" r="1.8" />
          <circle cx="12" cy="12" r="1.8" />
          <circle cx="12" cy="19" r="1.8" />
        </svg>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-40 mt-1.5 w-64 max-w-[88vw] overflow-hidden rounded-xl border border-neutral-700 bg-neutral-950 py-1 shadow-xl"
        >
          {shown.map((it) => (
            <button
              key={it.id}
              role="menuitem"
              disabled={it.disabled}
              onClick={() => {
                it.onClick()
                if (!it.keepOpen) setOpen(false)
              }}
              className={
                'flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition ' +
                'disabled:cursor-not-allowed disabled:opacity-40 ' +
                (it.active
                  ? 'bg-emerald-500/10 text-emerald-300 '
                  : it.danger
                    ? 'text-red-400 hover:bg-red-500/10 '
                    : 'text-neutral-200 hover:bg-neutral-800 ')
              }
            >
              <span className="flex h-4 w-4 shrink-0 items-center justify-center">{it.icon}</span>
              <span className="min-w-0 flex-1">
                <span className="block truncate">{it.label}</span>
                {it.hint && (
                  <span className="block truncate text-[11px] text-neutral-500">{it.hint}</span>
                )}
              </span>
              {it.keepOpen && (
                <span aria-hidden="true" className="shrink-0 text-[10px] text-neutral-500">
                  ▸
                </span>
              )}
            </button>
          ))}

          {footer && (
            <div
              // Clicks here must never close the menu: these widgets open
              // popovers of their own and need the menu to stay put.
              onClick={(e) => e.stopPropagation()}
              className="mt-1 border-t border-neutral-800 px-3 py-2"
            >
              {footer}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

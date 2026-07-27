import { useEffect, useRef, useState } from 'react'

// HeaderOverflow is the "⋯" button in the thread header.
//
// WhatsApp's thread header is deliberately sparse: avatar and name on the
// left, and on the right only video, voice and a single overflow menu. The
// bridge adds a lot of its own actions — extract tasks, run history, AI
// drafts, tasks, labels, members, scheduling, export, circles — and lining
// them all up in the header is the loudest tell that this is not WhatsApp.
//
// Rather than rewrite each action as a menu item (they carry real logic and
// their own conditional rendering), this hosts the existing buttons inside a
// popover. Same components, same handlers, WhatsApp's silhouette.
export function HeaderOverflow({
  children,
  label = 'More actions',
}: {
  children: React.ReactNode
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

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        onClick={() => setOpen((v) => !v)}
        title={label}
        aria-label={label}
        aria-expanded={open}
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
        // The actions keep their own button styling, so this is a container
        // rather than a menu: a wrapping row of the same controls.
        <div
          onClick={() => setOpen(false)}
          // An explicit width, not max-width: an absolutely positioned
          // flex-wrap container shrinks to its widest child otherwise, which
          // stacks every action into one cramped column.
          className="absolute right-0 top-full z-40 mt-1.5 flex w-[21rem] max-w-[88vw] flex-wrap items-center justify-start gap-1.5 rounded-xl border border-neutral-700 bg-neutral-950 p-2 shadow-xl"
        >
          {children}
        </div>
      )}
    </div>
  )
}

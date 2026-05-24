import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export type MenuItem =
  | {
      label: string
      icon?: string
      danger?: boolean
      disabled?: boolean
      onClick: () => void
    }
  | { divider: true }

// ContextMenu is a small floating menu shown at (x, y). It closes on outside
// click, Escape, scroll, or window blur. Used for right-click actions on chat
// rows, contact rows, etc.
export function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number
  y: number
  items: MenuItem[]
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ left: x, top: y })

  // Keep the menu inside the viewport.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const { width, height } = el.getBoundingClientRect()
    const pad = 6
    const vw = window.innerWidth
    const vh = window.innerHeight
    let left = x
    let top = y
    if (left + width + pad > vw) left = Math.max(pad, vw - width - pad)
    if (top + height + pad > vh) top = Math.max(pad, vh - height - pad)
    setPos({ left, top })
  }, [x, y])

  // Auto-close conditions.
  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    window.addEventListener('blur', onClose)
    document.addEventListener('scroll', onClose, true)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('blur', onClose)
      document.removeEventListener('scroll', onClose, true)
    }
  }, [onClose])

  return (
    <div
      ref={ref}
      role="menu"
      style={{ left: pos.left, top: pos.top }}
      className="fixed z-[60] min-w-[180px] overflow-hidden rounded-xl border border-neutral-700 bg-neutral-900 py-1 text-sm shadow-2xl shadow-black/60"
    >
      {items.map((item, i) =>
        'divider' in item ? (
          <div key={i} className="my-1 h-px bg-neutral-800" />
        ) : (
          <button
            key={i}
            role="menuitem"
            disabled={item.disabled}
            onClick={() => {
              if (!item.disabled) {
                item.onClick()
                onClose()
              }
            }}
            className={
              'flex w-full items-center gap-2 px-3 py-1.5 text-left transition disabled:cursor-not-allowed disabled:opacity-40 ' +
              (item.danger
                ? 'text-red-300 hover:bg-red-500/15'
                : 'text-neutral-200 hover:bg-neutral-800')
            }
          >
            {item.icon && <span className="w-4 shrink-0 text-center text-xs">{item.icon}</span>}
            <span className="flex-1 truncate">{item.label}</span>
          </button>
        ),
      )}
    </div>
  )
}

import { useEffect } from 'react'
import type { LightboxImage } from './ImageLightbox'

// SharedMediaModal is the grid that WhatsApp's chat info → "Media" tab
// pops to give you an overview of every image in a thread. We feed it
// the same lightboxImages MessageThread already computes for the in-chat
// lightbox, so there's no extra fetch and no risk of the grid drifting
// from what the bubbles render.
//
// Tapping a thumbnail closes the grid and re-opens the lightbox at that
// index — the existing ImageLightbox already does ←/→ navigation, so the
// gallery → image → swipe-through pattern matches WA cleanly with zero
// extra plumbing.
export function SharedMediaModal({
  title,
  images,
  onClose,
  onOpenIndex,
}: {
  title: string
  images: LightboxImage[]
  onClose: () => void
  onOpenIndex: (index: number) => void
}) {
  // Esc closes — same gesture every other overlay uses.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[640px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-neutral-100">
              Media in this chat
            </h2>
            <div dir="auto" className="truncate text-[11px] text-neutral-500">
              {title}
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span className="shrink-0 text-[11px] tabular-nums text-neutral-500">
              {images.length} {images.length === 1 ? 'photo' : 'photos'}
            </span>
            <button
              onClick={onClose}
              title="Close (Esc)"
              aria-label="Close"
              className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
            >
              ✕
            </button>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto p-3">
          {images.length === 0 ? (
            <div className="flex h-48 items-center justify-center text-xs text-neutral-600">
              No photos shared in this chat yet — they'll show up here as they arrive.
            </div>
          ) : (
            <div className="grid grid-cols-4 gap-1.5">
              {images.map((img, i) => (
                <button
                  key={img.id}
                  onClick={() => onOpenIndex(i)}
                  title={img.caption || ''}
                  className="group relative aspect-square overflow-hidden rounded-md bg-neutral-800 transition hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-emerald-500/50"
                >
                  <img
                    src={img.url}
                    alt={img.caption || ''}
                    loading="lazy"
                    className="h-full w-full object-cover transition-transform group-hover:scale-105"
                  />
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

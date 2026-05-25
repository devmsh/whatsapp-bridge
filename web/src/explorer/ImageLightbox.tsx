import { useEffect } from 'react'

// Single entry of the lightbox carousel — built from the messages array in the
// thread, so the order matches the chat's visual order.
export type LightboxImage = {
  id: string
  url: string
  caption?: string
  sender?: string
  timestamp?: number
}

// ImageLightbox renders a full-screen overlay with the currently selected
// image and ←/→/Esc/× controls. It mirrors official WhatsApp Web's photo
// viewer: dark backdrop, image centered with object-contain, caption strip at
// the bottom, prev/next arrows on the sides, close button in the top-right,
// and "n / total" counter so you can tell where you are in the carousel.
export function ImageLightbox({
  images,
  index,
  onIndex,
  onClose,
}: {
  images: LightboxImage[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
}) {
  const total = images.length
  const img = images[index]

  // Keyboard nav: ←/→ steps the carousel, Esc closes.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      } else if (e.key === 'ArrowLeft' && index > 0) {
        e.preventDefault()
        onIndex(index - 1)
      } else if (e.key === 'ArrowRight' && index < total - 1) {
        e.preventDefault()
        onIndex(index + 1)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [index, total, onIndex, onClose])

  if (!img) return null

  const hasPrev = index > 0
  const hasNext = index < total - 1

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-50 flex flex-col items-stretch bg-black/85 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
    >
      {/* Top bar: counter on the left, download + close on the right.
          Stops propagation so clicks on it don't dismiss the lightbox. */}
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex items-center justify-between px-5 py-3 text-sm text-neutral-200"
      >
        <span className="tabular-nums text-neutral-400">
          {index + 1} <span className="text-neutral-600">/ {total}</span>
        </span>
        <div className="flex items-center gap-1">
          <a
            href={img.url}
            download
            className="flex h-9 w-9 items-center justify-center rounded-full transition hover:bg-white/10"
            title="Download"
            aria-label="Download"
          >
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
          </a>
          <button
            onClick={onClose}
            className="flex h-9 w-9 items-center justify-center rounded-full transition hover:bg-white/10"
            title="Close (Esc)"
            aria-label="Close"
          >
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
      </div>

      {/* Centered image area. Clicking the image itself does not close —
          only the dark margin around it does. Prev/next arrows float on top. */}
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-12">
        {hasPrev && (
          <button
            onClick={(e) => {
              e.stopPropagation()
              onIndex(index - 1)
            }}
            className="absolute left-4 top-1/2 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/10 text-neutral-100 transition hover:bg-white/20"
            title="Previous (←)"
            aria-label="Previous image"
          >
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="15 18 9 12 15 6" />
            </svg>
          </button>
        )}
        <img
          src={img.url}
          alt=""
          onClick={(e) => e.stopPropagation()}
          className="max-h-full max-w-full object-contain"
        />
        {hasNext && (
          <button
            onClick={(e) => {
              e.stopPropagation()
              onIndex(index + 1)
            }}
            className="absolute right-4 top-1/2 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/10 text-neutral-100 transition hover:bg-white/20"
            title="Next (→)"
            aria-label="Next image"
          >
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="9 18 15 12 9 6" />
            </svg>
          </button>
        )}
      </div>

      {/* Caption / sender strip. Hidden when the image has no caption AND we
          have no sender info — keeps the viewer clean for raw photos. */}
      {(img.caption || img.sender) && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="mx-auto mb-4 max-w-3xl px-5 text-center text-sm text-neutral-200"
        >
          {img.caption && (
            <div dir="auto" className="leading-relaxed">
              {img.caption}
            </div>
          )}
          {img.sender && (
            <div className="mt-1 text-[11px] uppercase tracking-wider text-neutral-500">
              {img.sender}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

import { useEffect, useState } from 'react'
import { initial } from './format'

// ChatAvatar renders a chat/contact/group profile picture, falling back to a
// colored letter avatar when the image is unavailable. The bridge endpoint
// `/api/v2/avatars/{jid}` returns the cached JPEG (or 404 → letter fallback).
//
// `size` controls width/height in pixels (default 40). `group` chooses the
// fallback color palette.
//
// `clickable` opts the avatar into WA's "tap to see the photo" behavior —
// renders as a button and opens a full-screen preview when there's an
// actual image to show. We deliberately don't pass clickable in places
// where the avatar already sits inside another click target (chat header,
// chat-list rows): nested buttons are invalid HTML and the parent click
// is the more useful action there.
export function ChatAvatar({
  jid,
  title,
  group,
  size = 40,
  clickable = false,
  className = '',
}: {
  jid: string
  title: string
  group?: boolean
  size?: number
  clickable?: boolean
  className?: string
}) {
  const [failed, setFailed] = useState(false)
  const [preview, setPreview] = useState(false)
  const box =
    'flex shrink-0 items-center justify-center rounded-full overflow-hidden ' +
    (group ? 'bg-sky-600/30 text-sky-300' : 'bg-neutral-700 text-neutral-200') +
    ' ' +
    className

  const style = { width: size, height: size, fontSize: Math.round(size * 0.36) }

  if (failed || !jid) {
    return (
      <div className={box + ' font-semibold'} style={style}>
        {initial(title)}
      </div>
    )
  }

  // Preview only makes sense when there's a real photo (not the letter
  // fallback) and the parent asked for click-to-preview. Otherwise we
  // render the plain non-interactive div, same as before.
  if (clickable) {
    return (
      <>
        <button
          onClick={(e) => {
            e.stopPropagation()
            setPreview(true)
          }}
          title={`See ${title}'s photo`}
          aria-label={`See ${title}'s photo`}
          className={box + ' transition hover:opacity-80 focus:outline-none focus:ring-2 focus:ring-emerald-500/50'}
          style={style}
        >
          <img
            src={'/api/v2/avatars/' + encodeURIComponent(jid)}
            alt=""
            loading="lazy"
            onError={() => setFailed(true)}
            className="h-full w-full object-cover"
          />
        </button>
        {preview && (
          <AvatarPreview jid={jid} title={title} onClose={() => setPreview(false)} />
        )}
      </>
    )
  }

  return (
    <div className={box} style={style}>
      <img
        src={'/api/v2/avatars/' + encodeURIComponent(jid)}
        alt=""
        loading="lazy"
        onError={() => setFailed(true)}
        className="h-full w-full object-cover"
      />
    </div>
  )
}

// AvatarPreview is the full-screen profile-picture modal WA pops when you
// tap an avatar. Black backdrop + centered round photo + title below.
// Dismisses on Esc, backdrop click, or the ✕ button — same gestures the
// rest of our overlays use. Pure presentational; no API calls (we already
// have the avatar JPEG cached at the same URL ChatAvatar uses).
function AvatarPreview({
  jid,
  title,
  onClose,
}: {
  jid: string
  title: string
  onClose: () => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    // Prevent background scroll while the overlay owns the screen.
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
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/85 backdrop-blur-sm"
    >
      <button
        onClick={(e) => {
          e.stopPropagation()
          onClose()
        }}
        title="Close (Esc)"
        aria-label="Close preview"
        className="absolute right-4 top-4 flex h-10 w-10 items-center justify-center rounded-full bg-neutral-900/80 text-neutral-200 transition hover:bg-neutral-800"
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-w-[92vw] flex-col items-center gap-4"
      >
        <img
          src={'/api/v2/avatars/' + encodeURIComponent(jid)}
          alt={title}
          className="h-[min(70vh,520px)] w-[min(70vh,520px)] max-w-full rounded-full object-cover shadow-2xl shadow-black/60"
        />
        <div dir="auto" className="text-center text-base font-medium text-neutral-100">
          {title}
        </div>
      </div>
    </div>
  )
}

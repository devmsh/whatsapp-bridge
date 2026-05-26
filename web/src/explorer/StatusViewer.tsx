import { useEffect, useMemo, useRef, useState } from 'react'
import type { Message } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { mediaURL } from './format'

// StatusViewer is the WA "story viewer" overlay — full-screen card with
// progress bars at the top, one per update for the picked sender. Updates
// auto-advance; tap left / right to skip prev / next; Esc closes.
//
// Per-update duration matches WA mobile's defaults:
//   image / text → 5 s
//   video        → the video's actual duration (capped at 30 s)
//
// We deliberately don't auto-pause on hover — WA mobile pauses on
// touch-and-hold, but on web a hover-pause would be aggravating for users
// who move the mouse over the screen. Esc + click-outside both close.
export function StatusViewer({
  sender,
  senderName,
  updates,
  onClose,
}: {
  sender: string
  senderName: string
  /** Sorted oldest → newest, the slideshow plays in that order. */
  updates: Message[]
  onClose: () => void
}) {
  const [idx, setIdx] = useState(0)
  const cur = updates[idx]
  const total = updates.length

  // Tick the progress bar at 60 ms cadence; advance to the next slide when
  // it reaches the duration for the current item. Reset on every idx change
  // so cycling jumps cleanly. Tracked as a ratio (0..1) so the bar render
  // is a simple width %.
  const [progress, setProgress] = useState(0)
  const startedAtRef = useRef<number>(Date.now())
  const durationMs = useMemo(() => {
    if (!cur) return 5000
    // Videos: use the natural duration if we can sniff it; otherwise 5 s as a
    // safe default. The viewer doesn't currently inspect <video>.duration —
    // a later cycle can wire that, for now 5 s feels fine.
    if (cur.media_type === 'video') return 5000
    return 5000
  }, [cur])

  useEffect(() => {
    startedAtRef.current = Date.now()
    setProgress(0)
    const h = window.setInterval(() => {
      const elapsed = Date.now() - startedAtRef.current
      const next = Math.min(1, elapsed / durationMs)
      setProgress(next)
      if (next >= 1) {
        window.clearInterval(h)
        // Auto-advance — or close if this was the last update.
        setIdx((i) => {
          if (i + 1 >= total) {
            onClose()
            return i
          }
          return i + 1
        })
      }
    }, 60)
    return () => window.clearInterval(h)
  }, [idx, durationMs, total, onClose])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
      else if (e.key === 'ArrowLeft') setIdx((i) => Math.max(0, i - 1))
      else if (e.key === 'ArrowRight') setIdx((i) => Math.min(total - 1, i + 1))
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose, total])

  if (!cur) return null
  const url = mediaURL(cur.media_path, cur.chat_jid)
  const isVideo = cur.media_type === 'video'
  const isImage = cur.media_type === 'image'
  const caption = cur.media_caption || cur.content || ''
  const when = (() => {
    const d = new Date(cur.timestamp * 1000)
    return d.toLocaleString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
    })
  })()

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[90] flex items-center justify-center bg-black/90"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="relative flex h-full max-h-[100vh] w-full max-w-[460px] flex-col"
      >
        {/* Progress bars — one per update for this sender. All bars before
            `idx` are full; the bar at `idx` reflects the live progress;
            bars after are empty. */}
        <div className="flex shrink-0 gap-1 px-3 pt-3">
          {updates.map((_, i) => {
            const fill = i < idx ? 1 : i === idx ? progress : 0
            return (
              <div
                key={i}
                className="h-1 flex-1 overflow-hidden rounded-full bg-white/25"
              >
                <div
                  className="h-full bg-white transition-none"
                  style={{ width: `${(fill * 100).toFixed(2)}%` }}
                />
              </div>
            )
          })}
        </div>

        {/* Header — avatar + sender name + posted time + close button. */}
        <header className="flex shrink-0 items-center gap-3 px-3 pb-2 pt-3 text-white">
          <ChatAvatar jid={sender} title={senderName} size={32} />
          <div className="min-w-0 flex-1">
            <div dir="auto" className="truncate text-sm font-medium">{senderName}</div>
            <div className="text-[11px] text-white/60">{when}</div>
          </div>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-white/70 transition hover:bg-white/10 hover:text-white"
          >
            ✕
          </button>
        </header>

        {/* Media body — image / video / text. Click the left half goes
            back, the right half forwards (WA's invisible tap zones). */}
        <div className="relative flex-1 overflow-hidden">
          <div className="absolute inset-0 flex items-center justify-center">
            {isImage && url && (
              <img
                src={url}
                alt={caption || 'Status update'}
                className="max-h-full max-w-full object-contain"
              />
            )}
            {isVideo && url && (
              <video
                src={url}
                autoPlay
                muted
                playsInline
                controls={false}
                className="max-h-full max-w-full"
              />
            )}
            {!isImage && !isVideo && (
              <div
                dir="auto"
                className="px-6 text-center text-lg leading-snug text-white"
              >
                {caption || '(empty update)'}
              </div>
            )}
          </div>

          {/* Left / right tap zones for prev / next, invisible. */}
          <button
            onClick={() => setIdx((i) => Math.max(0, i - 1))}
            aria-label="Previous"
            className="absolute inset-y-0 left-0 w-1/3"
          />
          <button
            onClick={() => setIdx((i) => Math.min(total - 1, i + 1))}
            aria-label="Next"
            className="absolute inset-y-0 right-0 w-1/3"
          />
        </div>

        {/* Caption — overlaid on the bottom strip when present for media. */}
        {(isImage || isVideo) && caption && (
          <div
            dir="auto"
            className="shrink-0 bg-black/60 px-4 py-3 text-center text-sm text-white"
          >
            {caption}
          </div>
        )}
      </div>
    </div>
  )
}

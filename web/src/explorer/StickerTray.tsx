import { useEffect, useState } from 'react'
import { api, type RecentSticker } from '../api'
import { mediaURL } from './format'

// StickerTray — the small "Recents" sticker picker shown above the
// composer's emoji button. Loads the bridge's deduped list of seen
// stickers on mount, renders as a grid. Click a sticker → onPick fires
// with its server path, the composer drops it into /send with
// sticker:true (cycle 90 wire path).
//
// We don't ship server-side sticker packs in this cycle (would need
// whatsmeow's pack-resolution plumbing); "Recents" alone covers the
// dominant use-case of "send the same reaction sticker again."
export function StickerTray({
  onPick,
  onClose,
  jid,
}: {
  /** Called with the sticker's server-relative path. The caller is
   *  responsible for firing api.send(jid, '', { mediaPath, sticker:true }). */
  onPick: (path: string) => void
  onClose: () => void
  /** Used only to resolve per-chat unlock tokens for mediaURL — same
   *  way image bubbles do. */
  jid: string
}) {
  const [stickers, setStickers] = useState<RecentSticker[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .recentStickers(60)
      .then(setStickers)
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load'))
  }, [])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      onClick={(e) => e.stopPropagation()}
      className="flex max-h-[60vh] w-[320px] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
    >
      <header className="flex items-center justify-between border-b border-neutral-800 px-3 py-2">
        <div className="text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
          Recent stickers
        </div>
        <button
          onClick={onClose}
          title="Close"
          aria-label="Close"
          className="flex h-6 w-6 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
        >
          ✕
        </button>
      </header>
      <div className="flex-1 overflow-y-auto p-2">
        {error && (
          <div className="rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-[11px] text-red-300">
            {error}
          </div>
        )}
        {!error && stickers === null && (
          <div className="py-8 text-center text-[11px] text-neutral-500">Loading…</div>
        )}
        {stickers !== null && stickers.length === 0 && (
          <div className="py-8 text-center text-[11px] text-neutral-600">
            No stickers yet. Send or receive one — it'll appear here.
          </div>
        )}
        {stickers && stickers.length > 0 && (
          <div className="grid grid-cols-4 gap-1.5">
            {stickers.map((s) => {
              const url = mediaURL(s.path, jid)
              if (!url) return null
              return (
                <button
                  key={s.path}
                  onClick={() => onPick(s.path)}
                  title="Send this sticker"
                  className="aspect-square overflow-hidden rounded-md bg-neutral-800/50 p-1 transition hover:bg-neutral-700/60"
                >
                  <img src={url} alt="" className="h-full w-full object-contain" />
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

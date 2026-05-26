import { useEffect } from 'react'
import { WALLPAPERS } from '../hooks/useChatWallpaper'

// WallpaperPicker is the small popover-modal that lets the user paint
// their own message-area tint per chat. WA has had this since forever; it
// reads as a "this chat is mine" cue and helps tell heavy threads apart
// at a glance.
//
// Swatches sit in one row (so the modal stays a glance, not a maze).
// The active swatch wears a white ring; the "Default" swatch shows a
// little ⌀ glyph since no swatch color is the "no tint" state.
export function WallpaperPicker({
  active,
  title,
  onPick,
  onClose,
}: {
  active: string
  title: string
  onPick: (css: string) => void
  onClose: () => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/70 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex w-[440px] max-w-[94vw] flex-col gap-3 rounded-2xl border border-neutral-700 bg-neutral-900 p-4 shadow-2xl shadow-black/60"
      >
        <header className="flex items-baseline justify-between">
          <h2 className="text-sm font-semibold text-neutral-100">Chat wallpaper</h2>
          <div dir="auto" className="truncate text-[11px] text-neutral-500">
            {title}
          </div>
        </header>
        <div className="flex flex-wrap gap-2">
          {WALLPAPERS.map((w) => {
            const isActive = (active || '') === w.css
            return (
              <button
                key={w.id}
                onClick={() => {
                  onPick(w.css)
                  onClose()
                }}
                title={w.label}
                aria-label={`${w.label} wallpaper`}
                className={
                  'flex h-12 w-12 items-center justify-center rounded-xl border-2 transition ' +
                  (isActive
                    ? 'border-emerald-400 ring-2 ring-emerald-400/40'
                    : 'border-neutral-700 hover:border-neutral-500')
                }
                style={{
                  // Stack the tint atop the app's neutral background so the
                  // swatch reads exactly the way the chat will once applied.
                  background:
                    'linear-gradient(' +
                    (w.css || 'rgba(0,0,0,0)') +
                    ',' +
                    (w.css || 'rgba(0,0,0,0)') +
                    '), #171717',
                }}
              >
                {w.id === 'default' && (
                  <span className="text-xs text-neutral-500">⌀</span>
                )}
              </button>
            )
          })}
        </div>
        <p className="text-[11px] text-neutral-500">
          Just this chat — wallpapers are stored locally in your browser.
        </p>
      </div>
    </div>
  )
}

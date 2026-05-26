import { useCallback, useEffect, useState } from 'react'

// useChatWallpaper persists a per-chat background color in localStorage and
// keeps it live across tabs / panes. Returns the active color (a CSS value
// or empty string for "default — no tint") plus setters.
//
// Storage key: wa.wallpaper.<jid>. A custom event lets in-tab listeners
// pick up changes immediately — the same trick the drafts hook uses,
// since localStorage's native `storage` event only fires in *other* tabs.

const KEY_PREFIX = 'wa.wallpaper.'
const CHANGED_EVENT = 'wa.wallpaper-changed'

export function useChatWallpaper(jid: string): {
  color: string
  setColor: (c: string) => void
  clear: () => void
} {
  const [color, setLocal] = useState<string>(() => read(jid))

  useEffect(() => {
    setLocal(read(jid))
    function onChange(e: Event) {
      const target = (e as CustomEvent<string>).detail
      if (!target || target === jid) setLocal(read(jid))
    }
    function onStorage(e: StorageEvent) {
      if (!e.key) return
      if (e.key === KEY_PREFIX + jid) setLocal(read(jid))
    }
    window.addEventListener(CHANGED_EVENT, onChange)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener(CHANGED_EVENT, onChange)
      window.removeEventListener('storage', onStorage)
    }
  }, [jid])

  const setColor = useCallback(
    (c: string) => {
      try {
        if (c) localStorage.setItem(KEY_PREFIX + jid, c)
        else localStorage.removeItem(KEY_PREFIX + jid)
        window.dispatchEvent(new CustomEvent(CHANGED_EVENT, { detail: jid }))
      } catch {}
    },
    [jid],
  )

  const clear = useCallback(() => setColor(''), [setColor])

  return { color, setColor, clear }
}

function read(jid: string): string {
  try {
    return localStorage.getItem(KEY_PREFIX + jid) || ''
  } catch {
    return ''
  }
}

// Curated palette — kept short on purpose so the picker stays a glanceable
// strip rather than a maze. Each tint is alpha-soft so it sits behind the
// emerald / neutral bubbles without competing for attention. The "default"
// entry has empty color and is what we ship without an explicit pick.
export const WALLPAPERS: { id: string; label: string; css: string }[] = [
  { id: 'default',  label: 'Default',   css: '' },
  { id: 'mint',     label: 'Mint',      css: 'rgba(16, 185, 129, 0.10)' },
  { id: 'sand',     label: 'Sand',      css: 'rgba(251, 191, 36, 0.08)' },
  { id: 'lavender', label: 'Lavender',  css: 'rgba(168, 85, 247, 0.10)' },
  { id: 'rose',     label: 'Rose',      css: 'rgba(244, 63, 94, 0.09)' },
  { id: 'sky',      label: 'Sky',       css: 'rgba(56, 189, 248, 0.10)' },
  { id: 'graphite', label: 'Graphite',  css: 'rgba(120, 120, 120, 0.12)' },
]

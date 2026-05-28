import { useEffect, useState } from 'react'

// usePinnedMessages — pin specific messages in a chat (WhatsApp's "Pin
// message"). Stored client-side in localStorage, keyed by chat JID. We keep a
// content snapshot per pin so the banner renders even when the original
// message isn't in the currently-loaded window.
//
//   localStorage['wa.pinnedMessages'] = { "<jid>": [{ id, who, text, timestamp }] }
//
// Shared across instances/tabs via the 'wa.pinned-changed' + storage events.

export interface PinnedMessage {
  id: string
  /** Display sender ("You" / contact name). */
  who: string
  /** Snapshot of the body (or a media label) at pin time. */
  text: string
  timestamp: number
}
type Pins = Record<string, PinnedMessage[]>

const KEY = 'wa.pinnedMessages'
const CHANGED = 'wa.pinned-changed'

function read(): Pins {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) {
      const v = JSON.parse(raw)
      if (v && typeof v === 'object') return v as Pins
    }
  } catch {
    /* ignore */
  }
  return {}
}
function write(p: Pins): void {
  localStorage.setItem(KEY, JSON.stringify(p))
  window.dispatchEvent(new CustomEvent(CHANGED))
}

export function usePinnedMessages(jid: string) {
  const [all, setAll] = useState<Pins>(read)

  useEffect(() => {
    const reload = () => setAll(read())
    const onStorage = (e: StorageEvent) => {
      if (e.key === KEY) reload()
    }
    window.addEventListener(CHANGED, reload)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener(CHANGED, reload)
      window.removeEventListener('storage', onStorage)
    }
  }, [])

  const pins = all[jid] || []
  const pinnedIds = new Set(pins.map((p) => p.id))

  const pin = (m: PinnedMessage): void => {
    const cur = read()
    const list = cur[jid] || []
    if (list.some((p) => p.id === m.id)) return
    cur[jid] = [...list, m]
    write(cur)
  }
  const unpin = (id: string): void => {
    const cur = read()
    const list = (cur[jid] || []).filter((p) => p.id !== id)
    if (list.length) cur[jid] = list
    else delete cur[jid]
    write(cur)
  }

  return { pins, pinnedIds, pin, unpin }
}

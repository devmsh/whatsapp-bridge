import { useEffect, useState } from 'react'

// useQuickReplies — client-side canned responses (WhatsApp Business
// "Quick replies"). Stored in localStorage, so they need no bridge
// round-trip, work offline, and never touch the live WA session. Mirrors
// the useScheduledMessages client-queue pattern.
//
//   localStorage['wa.quickReplies'] = [{ id, title, text }, ...]
//
// Every hook instance shares state through the 'wa.quick-replies-changed'
// event, so the manager panel and the composer picker stay in sync live;
// the native 'storage' event keeps other tabs in sync too.

const KEY = 'wa.quickReplies'
const CHANGED = 'wa.quick-replies-changed'

export interface QuickReply {
  /** Stable client id. */
  id: string
  /** Short label shown in the picker (e.g. "Greeting"). */
  title: string
  /** The message body inserted into the composer. */
  text: string
}

function read(): QuickReply[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const v = JSON.parse(raw)
    return Array.isArray(v) ? (v as QuickReply[]) : []
  } catch {
    return []
  }
}

function write(list: QuickReply[]): void {
  localStorage.setItem(KEY, JSON.stringify(list))
  window.dispatchEvent(new CustomEvent(CHANGED))
}

function uid(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 7)
}

export function useQuickReplies() {
  const [replies, setReplies] = useState<QuickReply[]>(read)

  useEffect(() => {
    const reload = () => setReplies(read())
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

  const add = (title: string, text: string): void => {
    const t = text.trim()
    if (!t) return
    write([...read(), { id: uid(), title: title.trim() || t.slice(0, 24), text: t }])
  }
  const update = (id: string, title: string, text: string): void => {
    const t = text.trim()
    if (!t) return
    write(
      read().map((r) =>
        r.id === id ? { ...r, title: title.trim() || t.slice(0, 24), text: t } : r,
      ),
    )
  }
  const remove = (id: string): void => {
    write(read().filter((r) => r.id !== id))
  }

  return { replies, add, update, remove }
}

import { useEffect, useState } from 'react'
import { api } from '../api'

// useScheduledMessages — purely client-side "schedule send" queue.
//
// WhatsApp Business has a "schedule message" feature; the bridge has no
// equivalent, so we hold the queue in localStorage and fire each message
// from the running tab via api.send the moment its time arrives.
//
// Trade-off: only fires while *this* browser tab is open. Closing the tab
// pauses everything; the message goes out the moment the tab is reopened
// (we never drop). For "fire even when offline", the bridge would need a
// scheduler — out of scope for this cycle.
//
// Storage shape (single key, one global queue, sorted nothing):
//   localStorage['wa.scheduled'] = [
//     { id, jid, text, scheduled_at, media_path?, mentioned_jids? },
//     ...
//   ]
//
// The hook is mounted once at the app root (Explorer); every component
// that needs to read or mutate the queue calls `useScheduledMessages()`
// for the same shared state via the `wa.scheduled-changed` event.

const KEY = 'wa.scheduled'
const POLL_MS = 30_000

export interface ScheduledMessage {
  /** Stable client-side id — used as the key in lists and to cancel. */
  id: string
  jid: string
  text: string
  /** Unix seconds; UTC. */
  scheduled_at: number
  /** Optional attachment payload — the bridge path the upload already
   *  produced (we deliberately don't try to keep the File alive in
   *  localStorage; that means scheduled media has to be uploaded before
   *  scheduling, which the composer wires for us via api.upload). */
  media_path?: string
  /** Mentions when scheduling in a group. */
  mentioned_jids?: string[]
}

function readQueue(): ScheduledMessage[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}
function writeQueue(q: ScheduledMessage[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(q))
    window.dispatchEvent(new CustomEvent('wa.scheduled-changed'))
  } catch {}
}

// Sender lock — when the autopilot is mid-flight on one message we don't
// want to fire it twice if React mounts the hook in two places. Held at
// module scope (not React state) so every consumer of the hook shares it.
let sending = false

async function flushDue() {
  if (sending) return
  const now = Math.floor(Date.now() / 1000)
  const q = readQueue()
  const due = q.filter((m) => m.scheduled_at <= now)
  if (due.length === 0) return
  sending = true
  try {
    // Serial — keeps order honest and the bridge happy when a chat has
    // back-to-back scheduled messages.
    for (const msg of due) {
      try {
        await api.send(msg.jid, msg.text, {
          mediaPath: msg.media_path,
          mentionedJIDs: msg.mentioned_jids,
        })
      } catch (e) {
        // Don't drop on failure — leave the message in the queue so the
        // user can retry. Mark it visibly by leaving scheduled_at as-is;
        // the UI will keep showing it in red-overdue style.
        console.warn('Scheduled send failed', msg, e)
        // Bump the timer 5 min into the future so we don't hammer the
        // bridge every poll while it's still failing.
        const remaining = readQueue().map((m) =>
          m.id === msg.id ? { ...m, scheduled_at: Math.floor(Date.now() / 1000) + 300 } : m,
        )
        writeQueue(remaining)
        continue
      }
      // Success — remove from queue (re-read to avoid clobbering a parallel
      // add the user just made from the composer).
      const remaining = readQueue().filter((m) => m.id !== msg.id)
      writeQueue(remaining)
    }
  } finally {
    sending = false
  }
}

export function useScheduledMessages(): {
  queue: ScheduledMessage[]
  forJID: (jid: string) => ScheduledMessage[]
  schedule: (m: Omit<ScheduledMessage, 'id'>) => string
  cancel: (id: string) => void
} {
  const [queue, setQueue] = useState<ScheduledMessage[]>(() => readQueue())

  useEffect(() => {
    function refresh() {
      setQueue(readQueue())
    }
    window.addEventListener('wa.scheduled-changed', refresh)
    window.addEventListener('storage', refresh) // cross-tab sync
    return () => {
      window.removeEventListener('wa.scheduled-changed', refresh)
      window.removeEventListener('storage', refresh)
    }
  }, [])

  function schedule(m: Omit<ScheduledMessage, 'id'>): string {
    const id = 's_' + Math.random().toString(36).slice(2, 10) + Date.now().toString(36)
    const next: ScheduledMessage = { ...m, id }
    writeQueue([...readQueue(), next])
    return id
  }
  function cancel(id: string) {
    writeQueue(readQueue().filter((m) => m.id !== id))
  }
  function forJID(jid: string) {
    return queue.filter((m) => m.jid === jid).sort((a, b) => a.scheduled_at - b.scheduled_at)
  }

  return { queue, forJID, schedule, cancel }
}

// useScheduledAutopilot mounts once at app root. Sets up the polling timer
// that fires due messages and runs an immediate sweep on mount so a
// just-loaded tab catches up on anything that became due while closed.
export function useScheduledAutopilot() {
  useEffect(() => {
    void flushDue()
    const h = window.setInterval(flushDue, POLL_MS)
    // Also flush on visibility regain — the user just came back to the
    // tab, fires anything due immediately rather than waiting for the
    // next 30s tick.
    function onVis() {
      if (document.visibilityState === 'visible') void flushDue()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(h)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [])
}

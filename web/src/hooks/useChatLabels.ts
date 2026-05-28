import { useEffect, useState } from 'react'

// useChatLabels — WhatsApp-Business "labels": color-coded tags you stick on
// chats ("New customer", "Paid", …) to organize them. Stored client-side in
// localStorage (no bridge round-trip), like the other cycle features.
//
//   localStorage['wa.labels']            = [{ id, name, color }, ...]   (definitions)
//   localStorage['wa.labelAssignments']  = { "<jid>": ["labelId", ...] }
//
// Every hook instance shares state via the 'wa.labels-changed' event, so the
// chat-list dots, the header picker, and the manager all stay in sync live.

export interface ChatLabel {
  id: string
  name: string
  color: string
}
type Assignments = Record<string, string[]>

const DEFS_KEY = 'wa.labels'
const ASSIGN_KEY = 'wa.labelAssignments'
const CHANGED = 'wa.labels-changed'

// Swatch palette for new/edited labels — calm, high-contrast on the dark UI.
export const LABEL_PALETTE = [
  '#ef4444', '#f97316', '#f59e0b', '#22c55e', '#14b8a6',
  '#0ea5e9', '#3b82f6', '#8b5cf6', '#ec4899', '#94a3b8',
]

// WhatsApp Business ships these five labels out of the box; seed them so the
// feature is useful on first open instead of an empty list.
const DEFAULTS: ChatLabel[] = [
  { id: 'l_new_customer', name: 'New customer', color: '#0ea5e9' },
  { id: 'l_new_order', name: 'New order', color: '#8b5cf6' },
  { id: 'l_pending_payment', name: 'Pending payment', color: '#f59e0b' },
  { id: 'l_paid', name: 'Paid', color: '#22c55e' },
  { id: 'l_order_complete', name: 'Order complete', color: '#14b8a6' },
]

function readDefs(): ChatLabel[] {
  try {
    const raw = localStorage.getItem(DEFS_KEY)
    if (raw) {
      const v = JSON.parse(raw)
      if (Array.isArray(v)) return v as ChatLabel[]
    }
  } catch {
    /* fall through to seed */
  }
  // Seed silently (no event — this runs during the initial render).
  try {
    localStorage.setItem(DEFS_KEY, JSON.stringify(DEFAULTS))
  } catch {
    /* ignore */
  }
  return DEFAULTS
}

function readAssign(): Assignments {
  try {
    const raw = localStorage.getItem(ASSIGN_KEY)
    if (raw) {
      const v = JSON.parse(raw)
      if (v && typeof v === 'object') return v as Assignments
    }
  } catch {
    /* ignore */
  }
  return {}
}

function writeDefs(d: ChatLabel[]): void {
  localStorage.setItem(DEFS_KEY, JSON.stringify(d))
  window.dispatchEvent(new CustomEvent(CHANGED))
}
function writeAssign(a: Assignments): void {
  localStorage.setItem(ASSIGN_KEY, JSON.stringify(a))
  window.dispatchEvent(new CustomEvent(CHANGED))
}
function uid(): string {
  return 'l_' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
}

export function useChatLabels() {
  const [labels, setLabels] = useState<ChatLabel[]>(readDefs)
  const [assignments, setAssignments] = useState<Assignments>(readAssign)

  useEffect(() => {
    const reload = () => {
      setLabels(readDefs())
      setAssignments(readAssign())
    }
    const onStorage = (e: StorageEvent) => {
      if (e.key === DEFS_KEY || e.key === ASSIGN_KEY) reload()
    }
    window.addEventListener(CHANGED, reload)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener(CHANGED, reload)
      window.removeEventListener('storage', onStorage)
    }
  }, [])

  const toggle = (jid: string, labelId: string): void => {
    const cur = readAssign()
    const ids = new Set(cur[jid] || [])
    if (ids.has(labelId)) ids.delete(labelId)
    else ids.add(labelId)
    const next = { ...cur }
    if (ids.size) next[jid] = [...ids]
    else delete next[jid]
    writeAssign(next)
  }

  const addLabel = (name: string, color: string): void => {
    const n = name.trim()
    if (!n) return
    writeDefs([...readDefs(), { id: uid(), name: n, color }])
  }

  const removeLabel = (id: string): void => {
    writeDefs(readDefs().filter((l) => l.id !== id))
    // Strip the deleted label from every chat it was on.
    const cur = readAssign()
    let changed = false
    for (const k of Object.keys(cur)) {
      const filtered = cur[k].filter((x) => x !== id)
      if (filtered.length !== cur[k].length) {
        changed = true
        if (filtered.length) cur[k] = filtered
        else delete cur[k]
      }
    }
    if (changed) writeAssign(cur)
  }

  return { labels, assignments, toggle, addLabel, removeLabel }
}

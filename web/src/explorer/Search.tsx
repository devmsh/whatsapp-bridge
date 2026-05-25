import { useEffect, useRef, useState } from 'react'
import { api, type SearchHit } from '../api'
import { HiddenLockModal } from './HiddenLock'

// SearchBar is the top-bar universal search. Type a query; the dropdown lists
// matching contacts, groups, circles, tasks, and message snippets. Picking
// any result routes through onPick.
//
// Special: typing a digits-only query that looks like a PIN (4-12 digits)
// pops the lock-unlock modal pre-filled with that PIN — matches the WhatsApp
// pattern for revealing locked chats.
export function SearchBar({
  onPick,
}: {
  onPick: (h: SearchHit) => void
}) {
  const [q, setQ] = useState('')
  const [pinForUnlock, setPinForUnlock] = useState<string | null>(null)
  const [hits, setHits] = useState<SearchHit[]>([])
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)

  // Debounced search.
  useEffect(() => {
    if (!q.trim()) {
      setHits([])
      return
    }
    setBusy(true)
    const t = setTimeout(() => {
      api
        .search(q)
        .then((r) => setHits(r.hits || []))
        .finally(() => setBusy(false))
    }, 220)
    return () => clearTimeout(t)
  }, [q])

  // Close on outside click.
  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  // Group results by kind for nicer rendering.
  const grouped: Record<string, SearchHit[]> = { contact: [], group: [], circle: [], task: [], message: [] }
  for (const h of hits) (grouped[h.kind] ||= []).push(h)

  function handlePick(h: SearchHit) {
    onPick(h)
    setOpen(false)
    setQ('')
  }

  return (
    <div ref={boxRef} className="relative w-full">
      <input
        value={q}
        onChange={(e) => {
          setQ(e.target.value)
          setOpen(true)
        }}
        onFocus={() => q && setOpen(true)}
        onKeyDown={(e) => {
          // Enter on a digits-only query is treated as a PIN attempt.
          if (e.key === 'Enter' && /^\d{4,12}$/.test(q.trim())) {
            e.preventDefault()
            setPinForUnlock(q.trim())
            setQ('')
            setOpen(false)
          } else if (e.key === 'Escape') {
            setQ('')
            setOpen(false)
          }
        }}
        placeholder="🔎  Search people, groups, tasks, messages…"
        className="w-full rounded-lg border border-neutral-800 bg-neutral-900 px-3 py-1.5 pr-8 text-xs text-neutral-200 placeholder:text-neutral-600 focus:border-neutral-600 focus:outline-none"
      />
      {q && (
        <button
          onClick={() => {
            setQ('')
            setOpen(false)
          }}
          title="Clear (Esc)"
          className="absolute right-1.5 top-1/2 -translate-y-1/2 flex h-5 w-5 items-center justify-center rounded text-neutral-500 hover:bg-neutral-800 hover:text-neutral-200"
        >
          ✕
        </button>
      )}
      {open && q && (
        <div className="absolute left-0 right-0 top-full z-40 mt-1 max-h-[60vh] overflow-y-auto rounded-lg border border-neutral-800 bg-neutral-950 shadow-xl">
          {busy && hits.length === 0 && (
            <div className="p-3 text-xs text-neutral-500">Searching…</div>
          )}
          {!busy && hits.length === 0 && (
            <div className="p-3 text-xs text-neutral-500">No matches.</div>
          )}
          {hits.length > 0 && (
            <div className="flex flex-col">
              {(['contact', 'group', 'circle', 'task', 'message'] as const).map((kind) => {
                const items = grouped[kind] || []
                if (items.length === 0) return null
                return (
                  <div key={kind}>
                    <div className="bg-neutral-900/60 px-3 py-1 text-[10px] uppercase tracking-wide text-neutral-500">
                      {LABEL[kind]} · {items.length}
                    </div>
                    {items.map((h, i) => (
                      <button
                        key={kind + i + h.id}
                        onClick={() => handlePick(h)}
                        className="flex w-full items-start gap-2 border-b border-neutral-900 px-3 py-2 text-left hover:bg-neutral-900"
                      >
                        <span className="mt-0.5 shrink-0 text-base leading-none">
                          {ICON[kind]}
                        </span>
                        <span className="min-w-0 flex-1">
                          <span
                            dir="auto"
                            className="block truncate text-sm text-neutral-100"
                          >
                            {h.title}
                          </span>
                          {(h.subtitle || h.snippet) && (
                            <span
                              dir="auto"
                              className="block truncate text-[11px] text-neutral-500"
                            >
                              {h.subtitle || h.snippet}
                            </span>
                          )}
                        </span>
                      </button>
                    ))}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
      {pinForUnlock && (
        <HiddenLockModal
          prefilledPin={pinForUnlock}
          onUnlocked={() => setPinForUnlock(null)}
          onClose={() => setPinForUnlock(null)}
        />
      )}
    </div>
  )
}

const ICON: Record<SearchHit['kind'], string> = {
  contact: '👤',
  group: '👥',
  circle: '⭕',
  task: '✓',
  message: '💬',
}
const LABEL: Record<SearchHit['kind'], string> = {
  contact: 'Contacts',
  group: 'Groups',
  circle: 'Circles',
  task: 'Tasks',
  message: 'Messages',
}

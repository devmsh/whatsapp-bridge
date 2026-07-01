import { useEffect, useState } from 'react'
import { api, type Chat, type WorkingHoursConfig } from '../api'

// Day label for each weekday number (0=Sun..6=Sat).
// Friday (5) and Saturday (6) are tagged as weekend (MENA/KSA convention).
const DAYS: { num: number; label: string; hint?: string }[] = [
  { num: 0, label: 'Sun' },
  { num: 1, label: 'Mon' },
  { num: 2, label: 'Tue' },
  { num: 3, label: 'Wed' },
  { num: 4, label: 'Thu' },
  { num: 5, label: 'Fri', hint: 'weekend' },
  { num: 6, label: 'Sat', hint: 'weekend' },
]

// WorkingHours is a settings modal for the auto-mute feature.
// It follows the same modal shell as PrivacySettings / MediaSettings:
//   – fixed full-screen backdrop, click-outside closes
//   – Escape key closes
//   – scrollable inner body
//   – single Save button at the bottom
export function WorkingHours({ onClose }: { onClose: () => void }) {
  const [cfg, setCfg] = useState<WorkingHoursConfig | null>(null)
  const [chats, setChats] = useState<Chat[]>([])
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const [chatFilter, setChatFilter] = useState('')

  // Load working-hours config + chat list on mount.
  useEffect(() => {
    api
      .workingHours()
      .then(setCfg)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : 'Failed to load config'),
      )
    api
      .chats()
      .then(setChats)
      .catch(() => {})
  }, [])

  // Keyboard + scroll-lock.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  // --- helpers ---

  function setEnabled(v: boolean) {
    if (!cfg) return
    setCfg({ ...cfg, enabled: v })
    setSaved(false)
  }

  function setStart(v: string) {
    if (!cfg) return
    setCfg({ ...cfg, start: v })
    setSaved(false)
  }

  function setEnd(v: string) {
    if (!cfg) return
    setCfg({ ...cfg, end: v })
    setSaved(false)
  }

  function toggleDay(num: number) {
    if (!cfg) return
    const next = cfg.working_days.includes(num)
      ? cfg.working_days.filter((d) => d !== num)
      : [...cfg.working_days, num].sort((a, b) => a - b)
    setCfg({ ...cfg, working_days: next })
    setSaved(false)
  }

  function toggleChat(jid: string) {
    if (!cfg) return
    const next = cfg.chat_jids.includes(jid)
      ? cfg.chat_jids.filter((j) => j !== jid)
      : [...cfg.chat_jids, jid]
    setCfg({ ...cfg, chat_jids: next })
    setSaved(false)
  }

  async function save() {
    if (!cfg) return
    setSaving(true)
    setError('')
    try {
      const next = await api.setWorkingHours(cfg)
      setCfg(next)
      setSaved(true)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  // Filtered chat list for the picker.
  const filterLower = chatFilter.toLowerCase()
  const visibleChats = chats.filter(
    (c) =>
      !filterLower ||
      c.name.toLowerCase().includes(filterLower) ||
      c.jid.toLowerCase().includes(filterLower),
  )

  // O(1) per-row lookup instead of O(S) includes() inside the map.
  const selectedSet = new Set(cfg?.chat_jids ?? [])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[90vh] w-[560px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        {/* Header */}
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Working hours</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-4 py-4">
          {error && (
            <div className="mb-3 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}

          {!cfg && !error && (
            <div className="py-10 text-center text-xs text-neutral-500">Loading…</div>
          )}

          {cfg && (
            <>
              {/* Enable toggle */}
              <SectionTitle>Auto-mute</SectionTitle>
              <label className="mb-4 flex cursor-pointer items-center justify-between rounded-lg px-3 py-2 hover:bg-neutral-800">
                <div>
                  <p className="text-sm text-neutral-100">Enable working-hours mute</p>
                  <p className="text-[11px] text-neutral-500">
                    Selected chats are muted during working hours (quiet hours).
                  </p>
                </div>
                <input
                  type="checkbox"
                  checked={cfg.enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                  className="h-4 w-4 accent-emerald-500"
                />
              </label>

              {/* Time window */}
              <SectionTitle>Working window</SectionTitle>
              <div className="mb-4 flex items-center gap-4 rounded-lg border border-neutral-800 px-3 py-3">
                <div className="flex flex-1 flex-col gap-1">
                  <label className="text-[11px] text-neutral-500">Start</label>
                  <input
                    type="time"
                    value={cfg.start}
                    onChange={(e) => setStart(e.target.value)}
                    className="rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm text-neutral-100 focus:border-emerald-500 focus:outline-none"
                  />
                </div>
                <span className="mt-4 text-neutral-500">–</span>
                <div className="flex flex-1 flex-col gap-1">
                  <label className="text-[11px] text-neutral-500">End</label>
                  <input
                    type="time"
                    value={cfg.end}
                    onChange={(e) => setEnd(e.target.value)}
                    className="rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm text-neutral-100 focus:border-emerald-500 focus:outline-none"
                  />
                </div>
              </div>
              <p className="mb-4 text-[11px] text-neutral-500">
                Start must be before end (overnight windows are not supported). Time is server local
                time.
              </p>

              {/* Working days */}
              <SectionTitle>Working days</SectionTitle>
              <div className="mb-4 flex flex-wrap gap-2">
                {DAYS.map((d) => {
                  const active = cfg.working_days.includes(d.num)
                  return (
                    <button
                      key={d.num}
                      onClick={() => toggleDay(d.num)}
                      title={d.hint ? `${d.label} (${d.hint})` : d.label}
                      className={
                        'flex flex-col items-center rounded-lg border px-3 py-2 text-xs font-medium transition ' +
                        (active
                          ? 'border-emerald-500/60 bg-emerald-500/15 text-emerald-200'
                          : 'border-neutral-700 bg-neutral-950 text-neutral-400 hover:bg-neutral-800')
                      }
                    >
                      <span>{d.label}</span>
                      {d.hint && (
                        <span className="mt-0.5 text-[9px] uppercase tracking-wide opacity-60">
                          {d.hint}
                        </span>
                      )}
                    </button>
                  )
                })}
              </div>

              {/* Chat picker */}
              <SectionTitle>Chats to mute</SectionTitle>
              <p className="mb-2 text-[11px] text-neutral-500">
                Selected chats are muted during working hours. The feature only touches chats it
                muted itself — manual mutes are never overwritten.
              </p>

              {/* Filter input */}
              <input
                type="text"
                placeholder="Filter chats…"
                value={chatFilter}
                onChange={(e) => setChatFilter(e.target.value)}
                className="mb-2 w-full rounded-md border border-neutral-700 bg-neutral-950 px-3 py-1.5 text-sm text-neutral-100 placeholder-neutral-600 focus:border-emerald-500 focus:outline-none"
              />

              {/* Scrollable chat list */}
              <div className="max-h-52 overflow-y-auto rounded-lg border border-neutral-800">
                {chats.length === 0 && (
                  <div className="px-3 py-4 text-center text-xs text-neutral-500">
                    Loading chats…
                  </div>
                )}
                {chats.length > 0 && visibleChats.length === 0 && (
                  <div className="px-3 py-4 text-center text-xs text-neutral-500">
                    No chats match "{chatFilter}"
                  </div>
                )}
                {visibleChats.map((c) => {
                  const selected = selectedSet.has(c.jid)
                  return (
                    <label
                      key={c.jid}
                      className="flex cursor-pointer items-center justify-between border-b border-neutral-800/60 px-3 py-2 last:border-b-0 hover:bg-neutral-800"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm text-neutral-100">{c.name || c.jid}</p>
                        <p className="truncate text-[10px] text-neutral-500">{c.jid}</p>
                      </div>
                      <input
                        type="checkbox"
                        checked={selected}
                        onChange={() => toggleChat(c.jid)}
                        className="ml-3 h-4 w-4 shrink-0 accent-emerald-500"
                      />
                    </label>
                  )
                })}
              </div>

              {cfg.chat_jids.length > 0 && (
                <p className="mt-1 text-[11px] text-neutral-500">
                  {cfg.chat_jids.length} chat{cfg.chat_jids.length !== 1 ? 's' : ''} selected
                </p>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        {cfg && (
          <footer className="border-t border-neutral-800 px-4 py-3">
            <button
              onClick={save}
              disabled={saving}
              className="w-full rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400 disabled:opacity-50"
            >
              {saving ? 'Saving…' : saved ? 'Saved ✓' : 'Save'}
            </button>
          </footer>
        )}
      </div>
    </div>
  )
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mt-3 mb-2 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
      {children}
    </h3>
  )
}

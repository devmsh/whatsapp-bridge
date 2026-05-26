import { useEffect, useState } from 'react'

// ScheduleSendModal — pops over the composer when the user clicks the
// clock button. Three presets + custom datetime, picks emit the chosen
// epoch seconds back to the caller.
//
// "In 1 hour" — quick rescue. "Tomorrow morning" — 9 AM next calendar day.
// "Pick a time…" — toggles a native datetime-local input so the user can
// land on the minute they actually want. We don't over-engineer with a
// custom date picker; WA Business uses platform pickers too.
export function ScheduleSendModal({
  onClose,
  onPick,
}: {
  onClose: () => void
  /** Called with the chosen Unix-second timestamp once the user confirms. */
  onPick: (when: number) => void
}) {
  const [customMode, setCustomMode] = useState(false)
  const [custom, setCustom] = useState('')
  const [error, setError] = useState('')

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

  function pickPreset(when: Date) {
    onPick(Math.floor(when.getTime() / 1000))
    onClose()
  }

  function confirmCustom() {
    setError('')
    if (!custom) {
      setError('Pick a date and time first.')
      return
    }
    const dt = new Date(custom)
    if (Number.isNaN(dt.getTime())) {
      setError('That timestamp doesn’t parse.')
      return
    }
    if (dt.getTime() < Date.now() + 15_000) {
      setError('Pick a time at least a few seconds in the future.')
      return
    }
    onPick(Math.floor(dt.getTime() / 1000))
    onClose()
  }

  const in1h = new Date(Date.now() + 60 * 60 * 1000)
  const tom9am = (() => {
    const d = new Date()
    d.setDate(d.getDate() + 1)
    d.setHours(9, 0, 0, 0)
    return d
  })()

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[85] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex w-[400px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Schedule message</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex flex-col gap-1 p-3">
          <PresetRow
            primary="In 1 hour"
            secondary={fmtAbsolute(in1h)}
            onClick={() => pickPreset(in1h)}
          />
          <PresetRow
            primary="Tomorrow morning"
            secondary={fmtAbsolute(tom9am)}
            onClick={() => pickPreset(tom9am)}
          />
          <PresetRow
            primary="Pick a time…"
            secondary="Choose any future moment"
            onClick={() => setCustomMode((v) => !v)}
            active={customMode}
          />
          {customMode && (
            <div className="mt-2 flex flex-col gap-2 rounded-lg border border-neutral-800 bg-neutral-950 p-3">
              <input
                type="datetime-local"
                value={custom}
                onChange={(e) => setCustom(e.target.value)}
                min={toLocalISO(new Date(Date.now() + 60_000))}
                className="w-full rounded-md border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm text-neutral-100 outline-none focus:border-emerald-500"
              />
              {error && <div className="text-[11px] text-red-300">{error}</div>}
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => {
                    setCustomMode(false)
                    setError('')
                  }}
                  className="rounded-md border border-neutral-700 px-2.5 py-1 text-[11px] text-neutral-300 transition hover:bg-neutral-800"
                >
                  Cancel
                </button>
                <button
                  onClick={confirmCustom}
                  className="rounded-md bg-emerald-600 px-2.5 py-1 text-[11px] font-medium text-white transition hover:bg-emerald-500"
                >
                  Schedule
                </button>
              </div>
            </div>
          )}
        </div>

        <footer className="border-t border-neutral-800 px-4 py-2 text-[11px] text-neutral-500">
          The message fires from this browser tab. If the tab is closed when the
          time arrives, it goes out the moment you next open the chat.
        </footer>
      </div>
    </div>
  )
}

function PresetRow({
  primary,
  secondary,
  onClick,
  active = false,
}: {
  primary: string
  secondary: string
  onClick: () => void
  active?: boolean
}) {
  return (
    <button
      onClick={onClick}
      className={
        'flex items-center justify-between rounded-lg px-3 py-2 text-left transition ' +
        (active ? 'bg-emerald-500/15 text-emerald-200' : 'hover:bg-neutral-800/60 text-neutral-100')
      }
    >
      <div>
        <div className="text-sm">{primary}</div>
        <div className="text-[11px] text-neutral-500">{secondary}</div>
      </div>
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="opacity-50">
        <path d="m9 18 6-6-6-6" />
      </svg>
    </button>
  )
}

// fmtAbsolute renders "Today 18:42" / "Tomorrow 09:00" / "Wed 03 09:00" —
// concrete-enough to read at a glance, no second-guessing.
export function fmtAbsolute(d: Date): string {
  const now = new Date()
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  const tomorrow = new Date()
  tomorrow.setDate(now.getDate() + 1)
  const hh = d.getHours().toString().padStart(2, '0')
  const mm = d.getMinutes().toString().padStart(2, '0')
  const time = `${hh}:${mm}`
  if (sameDay(d, now)) return `Today ${time}`
  if (sameDay(d, tomorrow)) return `Tomorrow ${time}`
  return `${d.toLocaleDateString(undefined, { weekday: 'short', day: '2-digit', month: 'short' })} ${time}`
}

// toLocalISO converts a Date to the "YYYY-MM-DDTHH:mm" string the
// datetime-local input wants — built-in toISOString gives UTC which would
// confuse the user; we want the local zone displayed verbatim.
function toLocalISO(d: Date): string {
  const tz = d.getTimezoneOffset() * 60_000
  return new Date(d.getTime() - tz).toISOString().slice(0, 16)
}

import { useEffect, useState } from 'react'
import { api } from '../api'

// DisappearingSection is the small "Disappearing messages" row WA shows
// inside Contact info / Group info. Picking a duration starts the timer
// on the chat (PUT /chats/{jid}/disappearing) — every new message after
// the switch auto-deletes after the chosen window. Picking Off cancels it.
//
// WhatsApp only accepts four timer values:
//   0           Off
//   86400       24 hours
//   604800      7 days
//   7776000     90 days
//
// We mirror those exactly. Anything else is rejected by whatsmeow and
// would just bounce back as an error.
//
// The current value is fetched on mount via api.chat(jid) so the picker
// shows the right "current" pip even when the parent didn't pass it in.
// On a successful change, the local state updates immediately (optimistic)
// — if the PUT fails, we revert and surface the error inline.
//
// Self-contained: the parent only passes jid + group? for copy tweaks.
export function DisappearingSection({
  jid,
  isGroup,
}: {
  jid: string
  isGroup: boolean
}) {
  const [timer, setTimer] = useState<number | null>(null) // null while loading
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let cancelled = false
    api
      .chat(jid)
      .then((c) => {
        if (!cancelled) setTimer(c.disappearing_timer ?? 0)
      })
      .catch(() => {
        if (!cancelled) setTimer(0)
      })
    return () => {
      cancelled = true
    }
  }, [jid])

  async function setTo(next: number) {
    if (next === timer) {
      setOpen(false)
      return
    }
    const prev = timer
    setTimer(next) // optimistic
    setSaving(true)
    setError('')
    try {
      await api.chatDisappearing(jid, next)
      // Tell the rest of the app: the chats prop everyone else reads is now
      // stale for at least this row. Explorer listens and refetches /chats
      // so the header clock chip in MessageThread updates without the user
      // having to reopen the chat.
      window.dispatchEvent(
        new CustomEvent('wa.chats-changed', { detail: { jid, disappearing_timer: next } }),
      )
      setOpen(false)
    } catch (e) {
      setTimer(prev)
      setError(e instanceof Error ? e.message : 'Could not change timer')
    } finally {
      setSaving(false)
    }
  }

  const label =
    timer === null
      ? 'Loading…'
      : timer === 0
        ? 'Off'
        : timer === 86400
          ? '24 hours'
          : timer === 604800
            ? '7 days'
            : timer === 7776000
              ? '90 days'
              : `${Math.round(timer / 86400)} days`

  return (
    <section className="border-b border-neutral-800">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left transition hover:bg-neutral-800/40"
      >
        <span className="flex h-9 w-9 items-center justify-center rounded-full bg-amber-500/15 text-amber-300">
          {/* Clock-with-arrow glyph — matches WA's "Disappearing" icon. */}
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7v5l3 2" />
          </svg>
        </span>
        <div className="flex-1">
          <div className="text-sm text-neutral-100">Disappearing messages</div>
          <div className="text-[11px] text-neutral-500">
            {timer === 0
              ? 'Off — messages stay until deleted'
              : `New messages disappear after ${label}`}
          </div>
        </div>
        <span className="shrink-0 rounded-full bg-neutral-800 px-2 py-0.5 text-[11px] text-neutral-300">
          {timer === null ? '…' : label}
        </span>
      </button>

      {open && timer !== null && (
        <div className="grid grid-cols-2 gap-2 px-4 pb-4">
          <OptionPill label="Off" active={timer === 0} disabled={saving} onClick={() => setTo(0)} />
          <OptionPill label="24 hours" active={timer === 86400} disabled={saving} onClick={() => setTo(86400)} />
          <OptionPill label="7 days" active={timer === 604800} disabled={saving} onClick={() => setTo(604800)} />
          <OptionPill label="90 days" active={timer === 7776000} disabled={saving} onClick={() => setTo(7776000)} />
          <p className="col-span-2 text-[11px] text-neutral-500">
            {isGroup
              ? 'Only group admins can change this on most groups. The change is announced to everyone.'
              : 'Both sides see the change. Your existing messages stay; only new ones get the timer.'}
          </p>
          {error && (
            <p className="col-span-2 text-[11px] text-red-300">{error}</p>
          )}
        </div>
      )}
    </section>
  )
}

function OptionPill({
  label,
  active,
  disabled,
  onClick,
}: {
  label: string
  active: boolean
  disabled: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={
        'rounded-lg border px-3 py-1.5 text-xs font-medium transition disabled:opacity-50 ' +
        (active
          ? 'border-emerald-500/60 bg-emerald-500/15 text-emerald-200'
          : 'border-neutral-700 bg-neutral-900 text-neutral-200 hover:bg-neutral-800')
      }
    >
      {label}
    </button>
  )
}

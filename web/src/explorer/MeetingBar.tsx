import { useEffect, useState } from 'react'
import { api, type Meeting } from '../api'

// MeetingBar sits directly above the composer and names the meeting this chat
// is about: the next one coming up, or the one just held, within a week either
// way. Clicking it opens the full meeting page.
//
// It is dismissable, and a dismissal is remembered per meeting rather than per
// chat — a new meeting in the same conversation should still speak up. The
// record lives in localStorage because it is a per-viewer convenience, not
// shared state: dismissing it on this Mac should not hide it on the phone.

const KEY = 'wa.meetingbar.dismissed'

function readDismissed(): Set<number> {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return new Set()
    const ids = JSON.parse(raw)
    return Array.isArray(ids) ? new Set(ids) : new Set()
  } catch {
    return new Set()
  }
}

function dismiss(id: number) {
  try {
    const ids = readDismissed()
    ids.add(id)
    // Keep the list from growing forever; only the recent ones matter.
    localStorage.setItem(KEY, JSON.stringify([...ids].slice(-200)))
  } catch {
    /* storage blocked — the bar simply comes back next reload */
  }
}

// pick returns the one meeting worth naming. Preference order: happening now or
// next, then one still being negotiated (it needs a decision), then the most
// recent one held.
function pick(list: Meeting[], dismissed: Set<number>): Meeting | null {
  const live = list.filter(
    (m) => !dismissed.has(m.id) && m.status !== 'cancelled' && m.review_status !== 'rejected',
  )
  if (live.length === 0) return null
  const now = Date.now() / 1000
  const upcoming = live
    .filter((m) => m.starts_at && m.starts_at >= now - 3600)
    .sort((a, b) => (a.starts_at || 0) - (b.starts_at || 0))
  if (upcoming.length) return upcoming[0]
  const undated = live.filter((m) => !m.starts_at && m.status === 'proposed')
  if (undated.length) return undated[0]
  const past = live
    .filter((m) => m.starts_at)
    .sort((a, b) => (b.starts_at || 0) - (a.starts_at || 0))
  return past[0] || null
}

function label(m: Meeting): string {
  if (!m.starts_at) return 'Being arranged'
  const diff = m.starts_at * 1000 - Date.now()
  const mins = Math.round(diff / 60000)
  if (mins >= -60 && mins <= 60) return mins > 5 ? `in ${mins} min` : 'now'
  const d = new Date(m.starts_at * 1000)
  const time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  const days = Math.round(diff / 86400000)
  if (days === 0) return `today ${time}`
  if (days === 1) return `tomorrow ${time}`
  if (days === -1) return `yesterday ${time}`
  const day = d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  return days > 0 ? `${day} ${time}` : `${day}`
}

export function MeetingBar({
  chatJID,
  onOpen,
}: {
  chatJID: string
  onOpen: (meetingID: number) => void
}) {
  const [meeting, setMeeting] = useState<Meeting | null>(null)
  const [dismissed, setDismissed] = useState<Set<number>>(readDismissed)

  useEffect(() => {
    let cancelled = false
    api
      .meetingsForChat(chatJID, 7)
      .then((list) => {
        if (!cancelled) setMeeting(pick(list || [], dismissed))
      })
      .catch(() => {
        if (!cancelled) setMeeting(null)
      })
    return () => {
      cancelled = true
    }
  }, [chatJID, dismissed])

  if (!meeting) return null

  const soon = meeting.starts_at
    ? meeting.starts_at * 1000 - Date.now() < 3600_000 &&
      meeting.starts_at * 1000 - Date.now() > -3600_000
    : false
  const openRequirements = (meeting.items || []).filter(
    (i) => i.kind === 'requirement' && !i.done,
  ).length

  return (
    <div
      className={
        'flex items-center gap-2 border-t px-3 py-1.5 text-sm ' +
        (soon
          ? 'border-emerald-500/30 bg-emerald-500/10'
          : 'border-neutral-800 bg-neutral-900/60')
      }
    >
      <button
        onClick={() => onOpen(meeting.id)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        <span aria-hidden="true" className="shrink-0">
          🗓️
        </span>
        <span dir="auto" className="min-w-0 truncate font-medium">
          {meeting.title}
        </span>
        <span className={'shrink-0 text-xs ' + (soon ? 'text-emerald-600' : 'text-neutral-500')}>
          {label(meeting)}
        </span>
        {openRequirements > 0 && (
          <span className="shrink-0 rounded-full bg-amber-500/15 px-1.5 py-px text-[11px] text-amber-600">
            {openRequirements} to do first
          </span>
        )}
      </button>
      {meeting.link && (
        <a
          href={meeting.link}
          target="_blank"
          rel="noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="shrink-0 rounded-lg bg-emerald-500 px-2.5 py-1 text-xs font-medium text-neutral-950"
        >
          Join
        </a>
      )}
      <button
        onClick={() => {
          dismiss(meeting.id)
          setDismissed(readDismissed())
        }}
        title="Hide this"
        aria-label="Hide this meeting"
        className="shrink-0 rounded px-1.5 text-neutral-500 transition hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
  )
}

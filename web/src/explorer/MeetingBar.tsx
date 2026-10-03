import { useEffect, useMemo, useState } from 'react'
import { api, type Meeting } from '../api'

// MeetingBar sits directly above the composer and names the meeting this chat
// is about: the next one coming up, or the one just held, within a week either
// way. Clicking it opens the full meeting page.
//
// A chat can have several meetings (an old DM can easily have five). The bar
// shows one at a time, with a small switcher to move through the rest.
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

// A closed meeting is done: it is not something still ahead of us.
function isClosed(m: Meeting): boolean {
  return (
    m.status === 'held' || m.status === 'cancelled' || m.status === 'lapsed' || m.status === 'resolved'
  )
}

// buildList orders every meeting worth naming, best first. Preference order:
// happening now or next (soonest first), then ones still being negotiated
// (newest first — they need a decision), then closed or past ones (most
// recent first). Dismissed and rejected meetings are left out entirely.
//
// Element 0 is the same meeting the old single-pick logic used to choose, so
// a chat with one open meeting looks exactly as it did before.
function buildList(list: Meeting[], dismissed: Set<number>): Meeting[] {
  const live = list.filter((m) => !dismissed.has(m.id) && m.review_status !== 'rejected')
  const now = Date.now() / 1000

  const upcoming = live
    .filter((m) => !isClosed(m) && m.starts_at && m.starts_at >= now - 3600)
    .sort((a, b) => (a.starts_at || 0) - (b.starts_at || 0))

  const undated = live
    .filter(
      (m) => !isClosed(m) && !m.starts_at && (m.status === 'proposed' || m.status === 'confirmed'),
    )
    .sort((a, b) => (b.origin_ts || b.created_at || 0) - (a.origin_ts || a.created_at || 0))

  const shown = new Set([...upcoming, ...undated].map((m) => m.id))
  const rest = live
    .filter((m) => !shown.has(m.id))
    .sort(
      (a, b) =>
        (b.starts_at || b.origin_ts || b.created_at || 0) -
        (a.starts_at || a.origin_ts || a.created_at || 0),
    )

  // The bar shows up for the same chats it always did: one with a meeting
  // ahead, one being arranged, or a dated one that already passed. A chat that
  // only has a cancelled meeting, or a closed one that never had a date, stays
  // quiet — those can be reached with the arrows, but they do not raise the
  // bar on their own.
  const worthABar = [...upcoming, ...undated, ...rest].filter(
    (m) => m.status !== 'cancelled' && (!isClosed(m) || !!m.starts_at),
  )
  if (worthABar.length === 0) return []

  return [...upcoming, ...undated, ...rest]
}

function label(m: Meeting): string {
  // Agreed, but nobody named a time yet, is not the same as still arguing.
  if (!m.starts_at) return m.status === 'confirmed' ? 'Time not set' : 'Being arranged'
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

// closedLabel names a done meeting in plain words instead of a time.
function closedLabel(m: Meeting): string {
  switch (m.status) {
    case 'held':
      return 'Held'
    case 'cancelled':
      return 'Cancelled'
    case 'lapsed':
      return 'Went quiet'
    case 'resolved':
      return 'Settled in chat'
    default:
      return label(m)
  }
}

export function MeetingBar({
  chatJID,
  onOpen,
}: {
  chatJID: string
  onOpen: (meetingID: number) => void
}) {
  const [rawList, setRawList] = useState<Meeting[]>([])
  const [dismissed, setDismissed] = useState<Set<number>>(readDismissed)
  // The id of the meeting to show, if we already picked one. null means
  // "use the first one in the ordered list" — this is how a chatJID change,
  // or a meeting dropping out of the list, falls back to element 0.
  const [selectedID, setSelectedID] = useState<number | null>(null)

  useEffect(() => {
    // A new chat: forget the old selection and start from the top.
    setSelectedID(null)
  }, [chatJID])

  useEffect(() => {
    let cancelled = false
    api
      .meetingsForChat(chatJID, 7)
      .then((list) => {
        if (!cancelled) setRawList(list || [])
      })
      .catch(() => {
        if (!cancelled) setRawList([])
      })
    return () => {
      cancelled = true
    }
  }, [chatJID])

  const ordered = useMemo(() => buildList(rawList, dismissed), [rawList, dismissed])

  let index = selectedID == null ? -1 : ordered.findIndex((m) => m.id === selectedID)
  if (index < 0) index = 0
  const meeting = ordered[index] || null

  if (!meeting) return null

  const closed = isClosed(meeting)
  const soon =
    !closed && meeting.starts_at
      ? meeting.starts_at * 1000 - Date.now() < 3600_000 &&
        meeting.starts_at * 1000 - Date.now() > -3600_000
      : false
  const openRequirements = (meeting.items || []).filter(
    (i) => i.kind === 'requirement' && !i.done,
  ).length

  function goTo(e: React.MouseEvent, delta: number) {
    e.stopPropagation()
    e.preventDefault()
    if (ordered.length < 2) return
    const next = ordered[(index + delta + ordered.length) % ordered.length]
    setSelectedID(next.id)
  }

  return (
    <div
      className={
        'flex items-center gap-2 border-t px-3 py-1.5 text-sm ' +
        (closed
          ? 'border-neutral-800 bg-neutral-900/40'
          : soon
            ? 'border-emerald-500/30 bg-emerald-500/10'
            : 'border-neutral-800 bg-neutral-900/60')
      }
    >
      <button
        onClick={() => onOpen(meeting.id)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        <span aria-hidden="true" className={'shrink-0' + (closed ? ' opacity-60' : '')}>
          🗓️
        </span>
        <span
          dir="auto"
          className={'min-w-0 truncate font-medium' + (closed ? ' text-neutral-500' : '')}
        >
          {meeting.title}
        </span>
        <span
          className={
            'shrink-0 text-xs ' +
            (closed ? 'text-neutral-600' : soon ? 'text-emerald-600' : 'text-neutral-500')
          }
        >
          {closed ? closedLabel(meeting) : label(meeting)}
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
      {ordered.length > 1 && (
        <div className="flex shrink-0 items-center gap-0.5 text-xs text-neutral-500">
          <button
            onClick={(e) => goTo(e, -1)}
            aria-label="Previous meeting"
            className="rounded px-1 hover:text-neutral-200"
          >
            ‹
          </button>
          <span className="tabular-nums">
            {index + 1} / {ordered.length}
          </span>
          <button
            onClick={(e) => goTo(e, 1)}
            aria-label="Next meeting"
            className="rounded px-1 hover:text-neutral-200"
          >
            ›
          </button>
        </div>
      )}
      <button
        onClick={() => {
          // Move to the next meeting in the list before this one disappears,
          // so dismissing does not jump back to the start of the list.
          const next = ordered.length > 1 ? ordered[(index + 1) % ordered.length] : null
          dismiss(meeting.id)
          setDismissed(readDismissed())
          setSelectedID(next && next.id !== meeting.id ? next.id : null)
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

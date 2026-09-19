import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Meeting, type MeetingChange, type MeetingItem } from '../api'

// The Meetings screen: a list on the left of the main pane, one meeting open on
// the right. A meeting carries more than a task does — people, an agenda,
// entry requirements, a place, and a trail of messages from several chats — so
// it gets a real page rather than a row you expand.

const STATUS_STYLE: Record<string, string> = {
  proposed: 'bg-amber-500/15 text-amber-600',
  confirmed: 'bg-emerald-500/15 text-emerald-600',
  held: 'bg-neutral-800 text-neutral-400',
  cancelled: 'bg-red-500/15 text-red-500',
  // A meeting nobody followed up on. Quiet, like `held` — it is not an
  // error, just dead.
  lapsed: 'bg-neutral-800 text-neutral-500',
  // Settled in the chat itself, so no meeting was needed.
  resolved: 'bg-sky-500/15 text-sky-600',
}

// Where a change to a meeting came from.
const SOURCE_LABEL: Record<MeetingChange['source'], string> = {
  model: 'from chat',
  rule: 'auto',
  user: 'you',
}

const KIND_LABEL: Record<MeetingItem['kind'], string> = {
  agenda: 'Agenda',
  requirement: 'Before the meeting',
  next_step: 'Next steps',
}

function when(m: Meeting): string {
  if (!m.starts_at) {
    // Still being negotiated. The offered slots are the useful thing to show.
    if (m.time_options) {
      try {
        const opts = JSON.parse(m.time_options)
        if (Array.isArray(opts) && opts.length) return 'Options: ' + opts.join(' · ')
      } catch {
        /* stored as free text — fall through */
      }
      return 'Options: ' + m.time_options
    }
    return 'No time agreed'
  }
  const d = new Date(m.starts_at * 1000)
  return d.toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function relativeDay(ts?: number): string {
  if (!ts) return ''
  const days = Math.round((ts * 1000 - Date.now()) / 86400000)
  if (days === 0) return 'today'
  if (days === 1) return 'tomorrow'
  if (days === -1) return 'yesterday'
  if (days > 0) return `in ${days} days`
  return `${-days} days ago`
}

// A local-calendar-day key, "YYYY-MM-DD", built from the browser's own time
// zone (not UTC). Sorts correctly as a plain string.
function dayKey(ts: number): string {
  const d = new Date(ts * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// The label on a day group's sticky header.
function dayLabel(key: string): string {
  const [y, m, d] = key.split('-').map(Number)
  const day = new Date(y, m - 1, d)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const diffDays = Math.round((day.getTime() - today.getTime()) / 86400000)
  if (diffDays === 0) return 'Today'
  if (diffDays === 1) return 'Tomorrow'
  if (diffDays === -1) return 'Yesterday'
  const opts: Intl.DateTimeFormatOptions = { weekday: 'short', day: 'numeric', month: 'short' }
  if (y !== today.getFullYear()) opts.year = 'numeric'
  return day.toLocaleDateString(undefined, opts)
}

// Just the clock time, for a row inside a day group (the day is already in
// the group's header, so the row does not need to repeat it).
function clockTime(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

// Same shape as `when()` uses for a meeting's own time, for any timestamp.
function formatTs(ts: number): string {
  return new Date(ts * 1000).toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// A `starts_at` change stores the value as a string of seconds since epoch.
function formatChangeDate(v: string): string {
  const n = Number(v)
  if (!v || !n) return 'no date'
  return formatTs(n)
}

function orEmpty(v: string): string {
  return v ? v : 'empty'
}

// One readable line for a meeting_changes row.
function changeLine(c: MeetingChange): string {
  if (c.field === 'starts_at') return `${formatChangeDate(c.old_value)} -> ${formatChangeDate(c.new_value)}`
  if (c.field === 'status') return `${orEmpty(c.old_value)} -> ${orEmpty(c.new_value)}`
  if (c.field === 'agenda') return `Agenda + ${c.new_value}`
  return `${c.field}: ${orEmpty(c.old_value)} -> ${orEmpty(c.new_value)}`
}

function MeetingRow({
  m,
  active,
  onPick,
  timeLabel,
  note,
}: {
  m: Meeting
  active: boolean
  onPick: () => void
  /** Replaces the full when(m) text — e.g. just the clock time, inside a day
      group where the day is already shown in the group header. */
  timeLabel?: string
  /** An extra muted line under the status/time line, e.g. "found 12 days ago". */
  note?: string
}) {
  const people = m.participants?.length || 0
  const open = (m.items || []).filter((i) => i.kind === 'requirement' && !i.done).length
  return (
    <button
      onClick={onPick}
      className={
        'w-full border-b border-neutral-900 px-4 py-3 text-left transition ' +
        (active ? 'bg-neutral-800' : 'hover:bg-neutral-900/60')
      }
    >
      <div className="flex items-start gap-2">
        <div dir="auto" className="min-w-0 flex-1 truncate text-sm font-medium">
          {m.title}
        </div>
        {m.review_status === 'pending_review' && (
          <span className="shrink-0 rounded-full bg-amber-500/15 px-2 py-px text-[10px] font-semibold text-amber-600">
            review
          </span>
        )}
      </div>
      <div className="mt-1 flex items-center gap-2 text-xs text-neutral-500">
        <span className={'rounded-full px-1.5 py-px text-[10px] ' + (STATUS_STYLE[m.status] || '')}>
          {m.status}
        </span>
        <span className="truncate">{timeLabel ?? when(m)}</span>
      </div>
      {note && <div className="mt-0.5 text-xs text-neutral-600">{note}</div>}
      <div className="mt-0.5 text-xs text-neutral-600">
        {people > 0 && `${people} ${people === 1 ? 'person' : 'people'}`}
        {open > 0 && ` · ${open} to do first`}
        {m.mode === 'in_person' && ' · in person'}
      </div>
      {(m.circles?.length || 0) > 0 && (
        <div className="mt-1 flex flex-wrap gap-1">
          {m.circles!.slice(0, 3).map((c) => (
            <span
              key={c.id}
              dir="auto"
              className="flex items-center gap-1 text-[10px] text-neutral-500"
            >
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: c.color || '#737373' }}
              />
              {c.name}
            </span>
          ))}
        </div>
      )}
    </button>
  )
}

// One day's meetings, with a sticky header (day label + count) that stays
// pinned to the top of the scroll area while its meetings scroll past.
// The "No date" groups are not tracked by the date jump bar.
const noGroupRef = () => () => {}

function DayGroupSection({
  group,
  registerRef,
  selectedId,
  onSelect,
  byChat,
  nameMap,
  undated,
}: {
  group: { key: string; label: string; meetings: Meeting[] }
  registerRef: (key: string) => (el: HTMLDivElement | null) => void
  selectedId: number | null
  onSelect: (id: number | null) => void
  /** Split the day again by the chat the meeting came from. */
  byChat: boolean
  nameMap: Map<string, string>
  /** The "No date" list: the day is when the ORIGINAL MESSAGE was sent, so a
      row shows the offered options, not a clock time. */
  undated?: boolean
}) {
  const row = (m: Meeting, showChat: boolean) => (
    <MeetingRow
      key={m.id}
      m={m}
      active={selectedId === m.id}
      onPick={() => onSelect(m.id)}
      timeLabel={!undated && m.starts_at ? clockTime(m.starts_at) : undefined}
      note={showChat && m.origin_chat_jid ? displayName(m.origin_chat_jid, nameMap) : undefined}
    />
  )

  // Chats in the order their first meeting appears, so the day still reads
  // top to bottom in time.
  const chats: { jid: string; meetings: Meeting[] }[] = []
  if (byChat) {
    const at = new Map<string, number>()
    for (const m of group.meetings) {
      const jid = m.origin_chat_jid || ''
      const i = at.get(jid)
      if (i == null) {
        at.set(jid, chats.length)
        chats.push({ jid, meetings: [m] })
      } else chats[i].meetings.push(m)
    }
  }

  return (
    <div ref={registerRef(group.key)}>
      <div className="sticky top-0 z-10 border-b border-neutral-900 bg-neutral-950 px-4 py-1.5 text-xs font-medium text-neutral-400">
        {dayLabel(group.key)} <span className="text-neutral-600">· {group.meetings.length}</span>
      </div>
      {byChat
        ? chats.map((c) => (
            <div key={c.jid}>
              <div className="truncate border-b border-neutral-900 px-4 pb-1 pt-2 text-[11px] font-medium text-emerald-600">
                {c.jid ? displayName(c.jid, nameMap) : 'Unknown chat'}
                <span className="font-normal text-neutral-600"> · {c.meetings.length}</span>
              </div>
              {c.meetings.map((m) => row(m, false))}
            </div>
          ))
        : group.meetings.map((m) => row(m, !!undated))}
    </div>
  )
}

// The compact "jump between dates" line above the Dated list: prev/next
// arrows (moving one day-group at a time), the day at the top of the
// scroll area, a Today button, and a native date picker.
function DateJumpBar({
  groups,
  topGroupKey,
  atEnd,
  onPrev,
  onNext,
  onToday,
  onPickDate,
}: {
  groups: { key: string; label: string }[]
  topGroupKey: string | null
  /** The list is scrolled as far as it goes. The last day is often shorter
      than the screen, so its header never reaches the top; without this the
      next arrow stays on and does nothing. */
  atEnd: boolean
  onPrev: () => void
  onNext: () => void
  onToday: () => void
  onPickDate: (key: string) => void
}) {
  const idx = topGroupKey ? groups.findIndex((g) => g.key === topGroupKey) : -1
  const canPrev = idx > 0
  const canNext = idx >= 0 && idx < groups.length - 1 && !atEnd
  const label = idx >= 0 ? dayLabel(groups[idx].key) : ''
  return (
    <div className="flex items-center gap-1 border-b border-neutral-800 px-2 py-1.5">
      <button
        onClick={onPrev}
        disabled={!canPrev}
        aria-label="Previous day with meetings"
        className="shrink-0 rounded px-1.5 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800 disabled:opacity-30"
      >
        ‹
      </button>
      <span className="min-w-0 flex-1 truncate text-center text-xs text-neutral-300">{label}</span>
      <button
        onClick={onNext}
        disabled={!canNext}
        aria-label="Next day with meetings"
        className="shrink-0 rounded px-1.5 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800 disabled:opacity-30"
      >
        ›
      </button>
      <button
        onClick={onToday}
        className="shrink-0 rounded-full px-2 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800"
      >
        Today
      </button>
      <input
        type="date"
        onChange={(e) => {
          if (e.target.value) onPickDate(e.target.value)
          e.target.value = ''
        }}
        aria-label="Jump to date"
        className="w-[104px] shrink-0 rounded border border-neutral-800 bg-neutral-950 px-1 py-0.5 text-[11px] text-neutral-300 outline-none focus:border-neutral-600"
      />
    </div>
  )
}

function ItemList({
  meeting,
  kind,
  onChanged,
}: {
  meeting: Meeting
  kind: MeetingItem['kind']
  onChanged: () => void
}) {
  const [text, setText] = useState('')
  const items = (meeting.items || []).filter((i) => i.kind === kind)

  async function add() {
    const t = text.trim()
    if (!t) return
    setText('')
    await api.addMeetingItem(meeting.id, kind, t).catch(() => {})
    onChanged()
  }

  async function toggle(it: MeetingItem) {
    await api.updateMeetingItem(meeting.id, it.id, { done: !it.done }).catch(() => {})
    onChanged()
  }

  async function remove(it: MeetingItem) {
    await api.deleteMeetingItem(meeting.id, it.id).catch(() => {})
    onChanged()
  }

  return (
    <section className="mb-5">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        {KIND_LABEL[kind]}
      </h3>
      {items.length === 0 && <p className="mb-2 text-sm text-neutral-600">Nothing yet.</p>}
      {items.map((it) => (
        <div key={it.id} className="group flex items-start gap-2 py-1">
          <input
            type="checkbox"
            checked={it.done}
            onChange={() => toggle(it)}
            className="mt-1 h-3.5 w-3.5 shrink-0 accent-emerald-500"
          />
          <span
            dir="auto"
            className={'min-w-0 flex-1 text-sm ' + (it.done ? 'text-neutral-600 line-through' : '')}
          >
            {it.text}
          </span>
          <button
            onClick={() => remove(it)}
            title="Remove"
            className="shrink-0 text-xs text-neutral-600 opacity-0 transition group-hover:opacity-100 hover:text-red-500"
          >
            ✕
          </button>
        </div>
      ))}
      <input
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && add()}
        placeholder={`Add to ${KIND_LABEL[kind].toLowerCase()}…`}
        className="mt-2 w-full rounded-lg border border-neutral-800 bg-neutral-950 px-2.5 py-1.5 text-sm outline-none focus:border-neutral-600"
      />
    </section>
  )
}

// displayName falls back through the name map, then a readable phone number,
// never a bare JID.
function displayName(jid: string, nameMap: Map<string, string>): string {
  const known = nameMap.get(jid)
  if (known) return known
  const user = jid.split('@')[0]
  // A @lid identity has no phone meaning, so showing the digits would mislead.
  if (jid.endsWith('@lid')) return 'Unknown contact'
  return '+' + user
}

function MeetingHistory({
  changes,
  onOpenChat,
}: {
  changes: MeetingChange[]
  onOpenChat: (jid: string) => void
}) {
  if (changes.length === 0) return null
  const sorted = [...changes].sort((a, b) => b.created_at - a.created_at)
  return (
    <section className="mt-5">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        History
      </h3>
      <div className="space-y-2">
        {sorted.map((c) => (
          <div key={c.id} className="text-xs">
            <div className="flex items-center gap-2 text-neutral-500">
              <span>{formatTs(c.created_at)}</span>
              <span className="rounded-full bg-neutral-900 px-1.5 py-px text-[10px] text-neutral-400">
                {SOURCE_LABEL[c.source] || c.source}
              </span>
              {c.chat_jid && (
                <button
                  onClick={() => onOpenChat(c.chat_jid)}
                  className="rounded-full bg-neutral-900 px-1.5 py-px text-[10px] text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200"
                >
                  open chat
                </button>
              )}
            </div>
            <div dir="auto" className="mt-0.5 text-neutral-300">
              {changeLine(c)}
            </div>
            {c.note && (
              <div dir="auto" className="mt-0.5 text-neutral-600">
                {c.note}
              </div>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}

function MeetingDetail({
  meeting,
  nameMap,
  onChanged,
  onOpenChat,
  onOpenCircle,
}: {
  meeting: Meeting
  nameMap: Map<string, string>
  onChanged: () => void
  onOpenChat: (jid: string) => void
  onOpenCircle?: (id: number) => void
}) {
  const [notes, setNotes] = useState(meeting.notes || '')
  const [savedNote, setSavedNote] = useState(false)
  const [checking, setChecking] = useState(false)
  const [checkError, setCheckError] = useState('')
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollDeadline = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    setNotes(meeting.notes || '')
  }, [meeting.id, meeting.notes])

  function stopPolling() {
    if (pollTimer.current) clearInterval(pollTimer.current)
    if (pollDeadline.current) clearTimeout(pollDeadline.current)
    pollTimer.current = null
    pollDeadline.current = null
    setChecking(false)
  }

  // Clear timers if the user leaves this screen mid-check.
  useEffect(() => stopPolling, [])

  // The list reload closes over the active filter tab. The poll can outlive a
  // tab change, so it reads the newest callback from a ref.
  const onChangedRef = useRef(onChanged)
  onChangedRef.current = onChanged

  async function checkChat() {
    if (checking) return
    setCheckError('')
    setChecking(true) // before the request, so a double click sends one POST
    try {
      await api.meetingsRefresh({ meeting_id: meeting.id })
    } catch (e) {
      const status = (e as { status?: number } | undefined)?.status
      setCheckError(status === 409 ? 'A check is already running.' : 'Could not start the check.')
      setChecking(false)
      return
    }
    pollTimer.current = setInterval(async () => {
      // One failed status call is not "done". Keep polling; the deadline
      // below is what ends a check that never reports back.
      const s = await api.meetingsRefreshStatus().catch(() => null)
      if (s && !s.running && pollTimer.current) {
        stopPolling()
        onChangedRef.current()
      }
    }, 3000)
    pollDeadline.current = setTimeout(() => {
      stopPolling()
      onChangedRef.current()
    }, 3 * 60 * 1000)
  }

  async function saveNotes() {
    await api.updateMeeting(meeting.id, { notes }).catch(() => {})
    setSavedNote(true)
    setTimeout(() => setSavedNote(false), 1500)
    onChanged()
  }

  async function setStatus(status: Meeting['status']) {
    await api.updateMeeting(meeting.id, { status }).catch(() => {})
    onChanged()
  }

  async function review(status: 'accepted' | 'rejected') {
    await api.reviewMeeting(meeting.id, status).catch(() => {})
    onChanged()
  }

  // The chats a meeting was arranged across. More than one is the case a
  // calendar cannot represent at all, so it is worth showing plainly.
  const chats = useMemo(
    () => Array.from(new Set((meeting.messages || []).map((m) => m.chat_jid))),
    [meeting.messages],
  )

  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
      {meeting.review_status === 'pending_review' && (
        <div className="mb-4 flex items-center gap-2 rounded-lg bg-amber-500/10 px-3 py-2 text-sm">
          <span className="flex-1 text-amber-700">
            Found by AI{meeting.confidence ? ` · ${Math.round(meeting.confidence * 100)}% sure` : ''}.
            Check the date and people before trusting it.
          </span>
          <button
            onClick={() => review('accepted')}
            className="rounded-lg bg-emerald-500 px-3 py-1 text-xs font-medium text-neutral-950"
          >
            Keep
          </button>
          <button
            onClick={() => review('rejected')}
            className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Not a meeting
          </button>
        </div>
      )}

      <h2 dir="auto" className="text-lg font-semibold">
        {meeting.title}
      </h2>
      {meeting.purpose && (
        <p dir="auto" className="mt-1 text-sm text-neutral-400">
          {meeting.purpose}
        </p>
      )}

      <div className="mt-3 flex flex-wrap items-center gap-2">
        {(['proposed', 'confirmed', 'held', 'cancelled', 'lapsed', 'resolved'] as const).map((st) => (
          <button
            key={st}
            onClick={() => setStatus(st)}
            className={
              'rounded-full px-2.5 py-1 text-xs transition ' +
              (meeting.status === st
                ? STATUS_STYLE[st] + ' font-medium'
                : 'text-neutral-500 hover:bg-neutral-800')
            }
          >
            {st}
          </button>
        ))}
        <button
          onClick={checkChat}
          disabled={checking}
          className="ml-auto rounded-full border border-neutral-800 px-2.5 py-1 text-xs text-neutral-400 transition hover:bg-neutral-800 disabled:opacity-60"
        >
          {checking ? 'Checking…' : 'Check chat for updates'}
        </button>
      </div>
      {checkError && <div className="mt-1 text-xs text-red-500">{checkError}</div>}

      <dl className="mt-4 space-y-2 text-sm">
        <div className="flex gap-3">
          <dt className="w-24 shrink-0 text-neutral-500">When</dt>
          <dd dir="auto" className="min-w-0 flex-1">
            {when(meeting)}
            {meeting.starts_at ? (
              <span className="ml-2 text-xs text-neutral-500">{relativeDay(meeting.starts_at)}</span>
            ) : null}
          </dd>
        </div>
        {meeting.recurrence && (
          <div className="flex gap-3">
            <dt className="w-24 shrink-0 text-neutral-500">Repeats</dt>
            <dd dir="auto" className="min-w-0 flex-1">
              {meeting.recurrence}
            </dd>
          </div>
        )}
        {(meeting.location || meeting.location_url || meeting.location_lat) && (
          <div className="flex gap-3">
            <dt className="w-24 shrink-0 text-neutral-500">Where</dt>
            <dd dir="auto" className="min-w-0 flex-1">
              {meeting.location}
              {meeting.location_url && (
                <a
                  href={meeting.location_url}
                  target="_blank"
                  rel="noreferrer"
                  className="ml-2 text-emerald-600 hover:underline"
                >
                  Open map
                </a>
              )}
              {!meeting.location_url && meeting.location_lat ? (
                <a
                  href={`https://maps.google.com/?q=${meeting.location_lat},${meeting.location_lng}`}
                  target="_blank"
                  rel="noreferrer"
                  className="ml-2 text-emerald-600 hover:underline"
                >
                  Open map
                </a>
              ) : null}
            </dd>
          </div>
        )}
        {meeting.link && (
          <div className="flex gap-3">
            <dt className="w-24 shrink-0 text-neutral-500">Join</dt>
            <dd className="min-w-0 flex-1 truncate">
              <a
                href={meeting.link}
                target="_blank"
                rel="noreferrer"
                className="text-emerald-600 hover:underline"
              >
                {meeting.link}
              </a>
            </dd>
          </div>
        )}
        {(meeting.circles?.length || 0) > 0 && (
          <div className="flex gap-3">
            <dt className="w-24 shrink-0 text-neutral-500">Circles</dt>
            <dd className="min-w-0 flex-1">
              {meeting.circles!.map((c) => (
                <button
                  key={c.id}
                  dir="auto"
                  onClick={() => onOpenCircle?.(c.id)}
                  title="Open this circle"
                  className="mr-1.5 inline-flex items-center gap-1.5 rounded-full bg-neutral-900 px-2 py-0.5 text-xs text-neutral-300 transition hover:bg-neutral-800"
                >
                  <span
                    className="h-2 w-2 rounded-full"
                    style={{ backgroundColor: c.color || '#737373' }}
                  />
                  {c.name}
                </button>
              ))}
            </dd>
          </div>
        )}
        {chats.length > 0 && (
          <div className="flex gap-3">
            <dt className="w-24 shrink-0 text-neutral-500">
              {chats.length > 1 ? 'Arranged in' : 'From'}
            </dt>
            <dd className="min-w-0 flex-1">
              {chats.map((jid) => (
                <button
                  key={jid}
                  dir="auto"
                  onClick={() => onOpenChat(jid)}
                  title="Open this chat"
                  className="mr-1.5 rounded-full bg-neutral-800 px-2 py-0.5 text-xs text-neutral-300 hover:bg-neutral-700"
                >
                  {displayName(jid, nameMap)}
                </button>
              ))}
              {chats.length > 1 && (
                <span className="ml-1 text-xs text-neutral-500">
                  across {chats.length} chats
                </span>
              )}
            </dd>
          </div>
        )}
      </dl>

      {(meeting.participants?.length || 0) > 0 && (
        <section className="mt-5">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
            People
          </h3>
          <div className="flex flex-wrap gap-1.5">
            {meeting.participants!.map((p) => (
              <button
                key={p.jid}
                onClick={() => onOpenChat(p.jid)}
                title={p.note || 'Open chat'}
                dir="auto"
                className="rounded-full bg-neutral-900 px-2.5 py-1 text-xs transition hover:bg-neutral-800 hover:text-neutral-100"
              >
                {p.name || displayName(p.jid, nameMap)}
                {p.rsvp === 'yes' && <span className="ml-1 text-emerald-600">✓</span>}
                {p.rsvp === 'no' && <span className="ml-1 text-red-500">✕</span>}
                {p.rsvp === 'maybe' && <span className="ml-1 text-amber-600">?</span>}
              </button>
            ))}
          </div>
        </section>
      )}

      <div className="mt-5">
        <ItemList meeting={meeting} kind="requirement" onChanged={onChanged} />
        <ItemList meeting={meeting} kind="agenda" onChanged={onChanged} />
        <ItemList meeting={meeting} kind="next_step" onChanged={onChanged} />
      </div>

      <MeetingHistory changes={meeting.changes || []} onOpenChat={onOpenChat} />

      <section>
        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
          Notes
        </h3>
        <textarea
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          onBlur={saveNotes}
          dir="auto"
          rows={4}
          placeholder="What happened, decisions taken…"
          className="w-full rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-600"
        />
        {savedNote && <div className="mt-1 text-xs text-emerald-600">Saved</div>}
      </section>
    </div>
  )
}

export function MeetingsView({
  selectedId,
  nameMap,
  onSelect,
  onOpenChat,
  onOpenCircle,
}: {
  selectedId: number | null
  /** JID → display name, for groups and contacts alike. A raw JID is never
      something to show a person. */
  nameMap: Map<string, string>
  onSelect: (id: number | null) => void
  onOpenChat: (jid: string) => void
  onOpenCircle?: (id: number) => void
}) {
  const [meetings, setMeetings] = useState<Meeting[]>([])
  const [current, setCurrent] = useState<Meeting | null>(null)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'all' | 'upcoming' | 'review'>('upcoming')

  // The second row of tabs: dated meetings grouped by day, or the ones with
  // no date yet. `pickedSubTab` remembers whether the person chose this by
  // hand, so a filter change (or a reload) never overrides their pick.
  const [dateSubTab, setDateSubTabState] = useState<'dated' | 'nodate'>('dated')
  const pickedSubTab = useRef(false)

  function setDateSubTab(t: 'dated' | 'nodate') {
    pickedSubTab.current = true
    setDateSubTabState(t)
  }

  // Whether the Dated view should jump to "today" once the next render
  // commits. Set on a filter change or on switching into the Dated tab;
  // left alone by picking a meeting or by a same-filter reload.
  const autoScroll = useRef(true)

  function load() {
    setLoading(true)
    api
      .meetings(
        // "To review" means deciding whether a meeting AHEAD of you is real. A
        // meeting dated in the past has nothing left to accept, so it is not
        // asked about; it stays under "all".
        filter === 'review'
          ? { review: 'pending_review', upcoming: true }
          : filter === 'upcoming'
            ? { upcoming: true }
            : {},
      )
      .then((list) => setMeetings(list || []))
      .catch(() => setMeetings([]))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    autoScroll.current = true
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter])

  useEffect(() => {
    if (dateSubTab === 'dated') autoScroll.current = true
  }, [dateSubTab])

  // The loaded list has no say on which sub-tab the person already chose —
  // only picks the sensible default the first time.
  useEffect(() => {
    if (pickedSubTab.current) return
    const hasDated = meetings.some((m) => !!m.starts_at)
    const hasUndated = meetings.some((m) => !m.starts_at)
    setDateSubTabState(!hasDated && hasUndated ? 'nodate' : 'dated')
  }, [meetings])

  // The list carries participants and items, but not the message trail, so the
  // open meeting is always fetched in full.
  useEffect(() => {
    if (selectedId == null) {
      setCurrent(null)
      return
    }
    api.meeting(selectedId).then(setCurrent).catch(() => setCurrent(null))
  }, [selectedId, meetings])

  const pending = meetings.filter((m) => m.review_status === 'pending_review').length

  const dated = useMemo(() => meetings.filter((m) => !!m.starts_at), [meetings])
  const undated = useMemo(() => meetings.filter((m) => !m.starts_at), [meetings])

  const groups = useMemo(() => {
    const byDay = new Map<string, Meeting[]>()
    for (const m of dated) {
      const key = dayKey(m.starts_at!)
      const arr = byDay.get(key)
      if (arr) arr.push(m)
      else byDay.set(key, [m])
    }
    return Array.from(byDay.keys())
      .sort()
      .map((key) => ({
        key,
        label: dayLabel(key),
        meetings: byDay.get(key)!.sort((a, b) => (a.starts_at || 0) - (b.starts_at || 0)),
      }))
  }, [dated])

  // "No date" is ordered by when the conversation happened — the message that
  // caused the meeting — not by when the engine found it. A backfill finds a
  // June chat in September; that meeting is three months old, not new.
  const undatedGroups = useMemo(() => {
    const said = (m: Meeting) => m.origin_ts || m.created_at
    const byDay = new Map<string, Meeting[]>()
    for (const m of undated) {
      const key = dayKey(said(m))
      const arr = byDay.get(key)
      if (arr) arr.push(m)
      else byDay.set(key, [m])
    }
    return Array.from(byDay.keys())
      .sort()
      .reverse()
      .map((key) => ({
        key,
        label: dayLabel(key),
        meetings: byDay.get(key)!.sort((a, b) => said(b) - said(a)),
      }))
  }, [undated])

  // Remembered, because it is a way of reading the list, not a one-off.
  const [byChat, setByChat] = useState(() => {
    try {
      return localStorage.getItem('meetings.byChat') === '1'
    } catch {
      return false
    }
  })
  function toggleByChat() {
    setByChat((v) => {
      try {
        localStorage.setItem('meetings.byChat', v ? '0' : '1')
      } catch {
        /* private mode: the choice just does not stick */
      }
      return !v
    })
  }

  const listRef = useRef<HTMLDivElement | null>(null)
  const groupRefs = useRef(new Map<string, HTMLDivElement>())
  const [topGroupKey, setTopGroupKey] = useState<string | null>(null)
  const [atEnd, setAtEnd] = useState(false)
  const rafRef = useRef<number | null>(null)

  function registerGroupRef(key: string) {
    return (el: HTMLDivElement | null) => {
      if (el) groupRefs.current.set(key, el)
      else groupRefs.current.delete(key)
    }
  }

  function scrollToGroup(key: string) {
    groupRefs.current.get(key)?.scrollIntoView({ block: 'start' })
  }

  // Jumps to `key`'s group, or the first group after it, or the last group
  // when there is none after — the rule the Today button and the date
  // picker share.
  function scrollToDayOrAfter(key: string) {
    if (groups.length === 0) return
    const target = groups.find((g) => g.key >= key) ?? groups[groups.length - 1]
    scrollToGroup(target.key)
  }

  // Tracks which day group sits at the top of the scroll area, throttled to
  // one measurement per animation frame.
  useEffect(() => {
    if (dateSubTab !== 'dated') return
    const el = listRef.current
    if (!el) return

    function measure() {
      rafRef.current = null
      if (!el) return
      const containerTop = el.getBoundingClientRect().top
      let found: string | null = groups.length ? groups[0].key : null
      for (const g of groups) {
        const node = groupRefs.current.get(g.key)
        if (!node) continue
        if (node.getBoundingClientRect().top - containerTop <= 2) found = g.key
        else break
      }
      setTopGroupKey(found)
      setAtEnd(el.scrollTop + el.clientHeight >= el.scrollHeight - 1)
    }

    function onScroll() {
      if (rafRef.current != null) return
      rafRef.current = requestAnimationFrame(measure)
    }

    measure()
    el.addEventListener('scroll', onScroll)
    return () => {
      el.removeEventListener('scroll', onScroll)
      if (rafRef.current != null) {
        cancelAnimationFrame(rafRef.current)
        rafRef.current = null
      }
    }
  }, [dateSubTab, groups])

  // The once-per-load jump to "today" for the Dated view (item 4 of the
  // spec): fires after a filter change or a switch into this tab, never
  // after picking a meeting or a same-filter reload.
  useEffect(() => {
    if (!autoScroll.current) return
    if (dateSubTab !== 'dated') return
    if (groups.length === 0) return
    scrollToDayOrAfter(dayKey(Math.floor(Date.now() / 1000)))
    autoScroll.current = false
  }, [meetings, dateSubTab, groups])

  function goPrevDay() {
    const idx = topGroupKey ? groups.findIndex((g) => g.key === topGroupKey) : -1
    if (idx > 0) scrollToGroup(groups[idx - 1].key)
  }

  function goNextDay() {
    const idx = topGroupKey ? groups.findIndex((g) => g.key === topGroupKey) : -1
    if (idx >= 0 && idx < groups.length - 1) scrollToGroup(groups[idx + 1].key)
  }

  return (
    <div className="flex h-full min-h-0">
      <div className="flex w-80 shrink-0 flex-col border-r border-neutral-800">
        <header className="border-b border-neutral-800 px-4 py-3">
          <div className="text-sm font-semibold">Meetings</div>
          <div className="text-xs text-neutral-500">
            {loading ? 'Loading…' : `${meetings.length} shown`}
            {pending > 0 && ` · ${pending} to review`}
          </div>
        </header>
        <div className="flex gap-1 border-b border-neutral-800 px-2 py-2">
          {(['upcoming', 'review', 'all'] as const).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={
                'rounded-full px-2.5 py-1 text-xs transition ' +
                (filter === f
                  ? 'bg-emerald-500/15 font-medium text-emerald-600'
                  : 'text-neutral-400 hover:bg-neutral-800')
              }
            >
              {f === 'review' ? 'To review' : f}
            </button>
          ))}
        </div>
        <div className="flex gap-1 border-b border-neutral-800 px-2 py-2">
          {(
            [
              ['dated', `Dated (${dated.length})`],
              ['nodate', `No date (${undated.length})`],
            ] as const
          ).map(([t, text]) => (
            <button
              key={t}
              onClick={() => setDateSubTab(t)}
              className={
                'rounded-full px-2.5 py-1 text-xs transition ' +
                (dateSubTab === t
                  ? 'bg-emerald-500/15 font-medium text-emerald-600'
                  : 'text-neutral-400 hover:bg-neutral-800')
              }
            >
              {text}
            </button>
          ))}
          <button
            onClick={toggleByChat}
            title="Group each day by the chat the meeting came from"
            className={
              'ml-auto rounded-full px-2.5 py-1 text-xs transition ' +
              (byChat
                ? 'bg-emerald-500/15 font-medium text-emerald-600'
                : 'text-neutral-400 hover:bg-neutral-800')
            }
          >
            By chat
          </button>
        </div>
        {dateSubTab === 'dated' && groups.length > 0 && (
          <DateJumpBar
            groups={groups}
            topGroupKey={topGroupKey}
            atEnd={atEnd}
            onPrev={goPrevDay}
            onNext={goNextDay}
            onToday={() => scrollToDayOrAfter(dayKey(Math.floor(Date.now() / 1000)))}
            onPickDate={(iso) => scrollToDayOrAfter(iso)}
          />
        )}
        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto">
          {dateSubTab === 'dated' ? (
            !loading && groups.length === 0 ? (
              <div className="px-5 py-10 text-center text-sm text-neutral-600">
                No dated meetings here.
              </div>
            ) : (
              groups.map((g) => (
                <DayGroupSection
                  key={g.key}
                  group={g}
                  registerRef={registerGroupRef}
                  selectedId={selectedId}
                  onSelect={onSelect}
                  byChat={byChat}
                  nameMap={nameMap}
                />
              ))
            )
          ) : !loading && undatedGroups.length === 0 ? (
            <div className="px-5 py-10 text-center text-sm text-neutral-600">
              No meetings waiting for a date.
            </div>
          ) : (
            undatedGroups.map((g) => (
              <DayGroupSection
                key={g.key}
                group={g}
                registerRef={noGroupRef}
                selectedId={selectedId}
                onSelect={onSelect}
                byChat={byChat}
                nameMap={nameMap}
                undated
              />
            ))
          )}
        </div>
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {current ? (
          <MeetingDetail
            // A new meeting is a new panel: a check started on one meeting must
            // not show "Checking…" on the next one.
            key={current.id}
            meeting={current}
            nameMap={nameMap}
            onChanged={load}
            onOpenChat={onOpenChat}
            onOpenCircle={onOpenCircle}
          />
        ) : (
          <div className="flex flex-1 items-center justify-center px-6 text-center text-sm text-neutral-600">
            Pick a meeting to see its agenda, people and the messages that arranged it.
          </div>
        )}
      </div>
    </div>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { api, type Meeting, type MeetingItem } from '../api'

// The Meetings screen: a list on the left of the main pane, one meeting open on
// the right. A meeting carries more than a task does — people, an agenda,
// entry requirements, a place, and a trail of messages from several chats — so
// it gets a real page rather than a row you expand.

const STATUS_STYLE: Record<string, string> = {
  proposed: 'bg-amber-500/15 text-amber-600',
  confirmed: 'bg-emerald-500/15 text-emerald-600',
  held: 'bg-neutral-800 text-neutral-400',
  cancelled: 'bg-red-500/15 text-red-500',
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

function MeetingRow({
  m,
  active,
  onPick,
}: {
  m: Meeting
  active: boolean
  onPick: () => void
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
        <span className="truncate">{when(m)}</span>
      </div>
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

  useEffect(() => {
    setNotes(meeting.notes || '')
  }, [meeting.id, meeting.notes])

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
        {(['proposed', 'confirmed', 'held', 'cancelled'] as const).map((st) => (
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
      </div>

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

  function load() {
    setLoading(true)
    api
      .meetings(
        filter === 'review'
          ? { review: 'pending_review' }
          : filter === 'upcoming'
            ? { upcoming: true }
            : {},
      )
      .then((list) => setMeetings(list || []))
      .catch(() => setMeetings([]))
      .finally(() => setLoading(false))
  }

  useEffect(load, [filter])

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
        <div className="min-h-0 flex-1 overflow-y-auto">
          {!loading && meetings.length === 0 && (
            <div className="px-5 py-10 text-center text-sm text-neutral-600">
              {filter === 'review' ? 'Nothing waiting to be reviewed.' : 'No meetings yet.'}
            </div>
          )}
          {meetings.map((m) => (
            <MeetingRow
              key={m.id}
              m={m}
              active={selectedId === m.id}
              onPick={() => onSelect(m.id)}
            />
          ))}
        </div>
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {current ? (
          <MeetingDetail
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

import { useEffect, useMemo, useState } from 'react'
import { api, type CallEvent } from '../api'
import { chatListTime, isGroup, senderTitle } from './format'
import { ChatAvatar } from './ChatAvatar'

// CallsPanel is the sidebar's Calls tab — WA's "Calls" screen, condensed
// to the desktop bridge's reality (no initiate / call-back yet, since
// whatsmeow doesn't expose voice IO here). All we get from the bridge is
// the events.Call stream stored in the calls table, so we summarise:
// per real-world call (one call_id, many event rows), render one row
// with caller, status, and time. Clicking the row opens that contact's
// DM — the natural next action even without a call-back button.
export function CallsPanel({
  nameMap,
  onOpenChat,
}: {
  nameMap: Map<string, string>
  onOpenChat: (jid: string) => void
}) {
  const [events, setEvents] = useState<CallEvent[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api.calls(200).then((e) => {
      if (!cancelled) setEvents(e || [])
    }).catch((e) => {
      if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load calls')
    })
    return () => { cancelled = true }
  }, [])

  // Collapse the event stream into one summary per real call. We pick the
  // strongest final state: answered > rejected > missed. Duration is
  // accept→terminate when both exist, otherwise unknown.
  const calls = useMemo(() => summarise(events || []), [events])

  if (error) {
    return <div className="p-4 text-center text-xs text-red-400">{error}</div>
  }
  if (events === null) {
    return <div className="p-4 text-center text-xs text-neutral-600">Loading calls…</div>
  }
  if (calls.length === 0) {
    return (
      <div className="p-6 text-center text-xs text-neutral-600">
        No call history
        <div className="mt-1 text-neutral-700">
          (incoming calls only — the bridge can observe but not place calls)
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      {calls.map((c) => (
        <CallRow
          key={c.callId + c.startedAt}
          call={c}
          nameMap={nameMap}
          onOpenChat={onOpenChat}
        />
      ))}
    </div>
  )
}

interface CallSummary {
  callId: string
  fromJID: string
  groupJID?: string
  status: 'answered' | 'rejected' | 'missed' | 'ringing'
  startedAt: number
  endedAt?: number
}

function summarise(events: CallEvent[]): CallSummary[] {
  type Bucket = {
    callId: string
    fromJID: string
    groupJID?: string
    types: Set<string>
    firstTs: number
    lastTs: number
  }
  const map = new Map<string, Bucket>()
  for (const e of events) {
    if (!e.call_id) continue
    const b = map.get(e.call_id) || {
      callId: e.call_id,
      fromJID: e.from_jid,
      groupJID: e.group_jid,
      types: new Set<string>(),
      firstTs: e.timestamp,
      lastTs: e.timestamp,
    }
    b.types.add(e.event_type)
    if (e.timestamp < b.firstTs) b.firstTs = e.timestamp
    if (e.timestamp > b.lastTs) b.lastTs = e.timestamp
    // Keep from_jid if not yet set (initial offer carries it).
    if (!b.fromJID && e.from_jid) b.fromJID = e.from_jid
    map.set(e.call_id, b)
  }
  const out: CallSummary[] = []
  for (const b of map.values()) {
    let status: CallSummary['status']
    if (b.types.has('accept') || b.types.has('preaccept')) status = 'answered'
    else if (b.types.has('reject')) status = 'rejected'
    else if (b.types.has('timeout') || b.types.has('terminate')) status = 'missed'
    else status = 'ringing'
    out.push({
      callId: b.callId,
      fromJID: b.fromJID,
      groupJID: b.groupJID,
      status,
      startedAt: b.firstTs,
      endedAt: b.lastTs > b.firstTs ? b.lastTs : undefined,
    })
  }
  // Newest first — matches WA's own Calls screen ordering.
  out.sort((a, b) => b.startedAt - a.startedAt)
  return out
}

function CallRow({
  call,
  nameMap,
  onOpenChat,
}: {
  call: CallSummary
  nameMap: Map<string, string>
  onOpenChat: (jid: string) => void
}) {
  const isGroupCall = !!call.groupJID && isGroup(call.groupJID)
  const targetJID = isGroupCall && call.groupJID ? call.groupJID : call.fromJID
  const title = senderTitle(call.fromJID, '', '', nameMap)
  const subtitle = isGroupCall
    ? 'Group call · ' + senderTitle(call.groupJID || '', '', '', nameMap)
    : ''

  const { icon, label, tone } = statusBits(call.status)
  const duration =
    call.status === 'answered' && call.endedAt
      ? humanDur(call.endedAt - call.startedAt)
      : ''

  return (
    <button
      onClick={() => onOpenChat(targetJID)}
      className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition hover:bg-neutral-900"
    >
      <ChatAvatar jid={targetJID} title={title} group={isGroupCall} size={40} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span dir="auto" className="truncate text-sm font-medium text-neutral-100">
            {title}
          </span>
          <span className="shrink-0 text-[11px] text-neutral-500">
            {chatListTime(call.startedAt)}
          </span>
        </div>
        <div className="flex items-center gap-1.5 truncate text-xs">
          <span className={tone} aria-hidden="true">{icon}</span>
          <span className={tone}>{label}</span>
          {duration && <span className="text-neutral-500">· {duration}</span>}
          {subtitle && <span className="truncate text-neutral-500">· {subtitle}</span>}
        </div>
      </div>
    </button>
  )
}

function statusBits(s: CallSummary['status']) {
  switch (s) {
    case 'answered':
      return { icon: '↙', label: 'Answered', tone: 'text-emerald-300' }
    case 'rejected':
      return { icon: '⊘', label: 'Declined', tone: 'text-neutral-400' }
    case 'missed':
      return { icon: '↙', label: 'Missed', tone: 'text-red-300' }
    case 'ringing':
      return { icon: '∿', label: 'Ringing', tone: 'text-amber-300' }
  }
}

function humanDur(sec: number): string {
  if (sec <= 0) return ''
  const m = Math.floor(sec / 60)
  const s = sec % 60
  if (m === 0) return `${s}s`
  return `${m}m ${s}s`
}

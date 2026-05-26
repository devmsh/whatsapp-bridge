import { useEffect, useMemo, useState } from 'react'
import { api, type Message, type PollDetail } from '../api'

// PollBubble renders a WA poll inline inside a MessageBubble. Pulls the
// poll body + vote tallies from /api/v2/polls/:id on mount, presents
// each option as a row with a vote bar + count, and lets the user cast
// (or change) a vote. Single-choice (max_selections === 1) replaces the
// previous pick; multi-choice toggles.
//
// We optimistically update the local tallies on vote so the bar moves
// the moment the user clicks, then revert if the API call fails. Real
// WA polls are end-to-end encrypted and counted client-side — same
// shape here.
export function PollBubble({
  msg,
  mine,
  selfJID,
}: {
  msg: Message
  mine: boolean
  /** The current user's voter JID. Used to highlight your own pick.
   *  Optional — if missing we just skip the "your vote" emphasis. */
  selfJID?: string
}) {
  const [detail, setDetail] = useState<PollDetail | null>(null)
  const [loadErr, setLoadErr] = useState('')
  const [voting, setVoting] = useState(false)

  useEffect(() => {
    let cancelled = false
    api.getPoll(msg.chat_jid, msg.id).then((d) => {
      if (cancelled) return
      if (!d) setLoadErr('Could not load poll')
      else setDetail(d)
    }).catch((e) => {
      if (!cancelled) setLoadErr(e instanceof Error ? e.message : 'Failed')
    })
    return () => { cancelled = true }
  }, [msg.id, msg.chat_jid])

  // Parse options from the JSON string the bridge stores. Defensively
  // accept malformed input — never crash on bad data.
  const options = useMemo<string[]>(() => {
    if (!detail) return []
    try {
      const parsed = JSON.parse(detail.poll.options)
      if (Array.isArray(parsed)) return parsed.filter((s) => typeof s === 'string')
    } catch {}
    return []
  }, [detail])

  // Tally votes per option index by parsing each voter's selected_options
  // JSON. Tracks the current user's picks too so the rows highlight what
  // they chose. Multi-choice polls (max_selections > 1) split a single
  // voter across rows, which is why we count per (voter × pick) cell.
  const { counts, totalVoters, myPicks } = useMemo(() => {
    const counts = new Map<string, number>() // option text → count
    const my = new Set<string>()
    const voters = new Set<string>()
    for (const v of detail?.votes || []) {
      voters.add(v.voter_jid)
      let picks: string[] = []
      try {
        const parsed = JSON.parse(v.selected_options)
        if (Array.isArray(parsed)) picks = parsed.filter((s) => typeof s === 'string')
      } catch {}
      for (const p of picks) {
        counts.set(p, (counts.get(p) || 0) + 1)
        if (selfJID && voter(v.voter_jid) === voter(selfJID)) my.add(p)
      }
    }
    return { counts, totalVoters: voters.size, myPicks: my }
  }, [detail, selfJID])

  async function cast(option: string) {
    if (!detail || voting) return
    const max = Math.max(1, detail.poll.max_selections || 1)
    let next: string[]
    if (max === 1) {
      // Single-choice: tapping a row replaces any previous pick.
      next = [option]
    } else {
      // Multi-choice: toggle, capped at max_selections.
      next = myPicks.has(option)
        ? Array.from(myPicks).filter((o) => o !== option)
        : [...Array.from(myPicks), option].slice(-max)
    }

    // Optimistic update: synthesise a vote row for self so the bars move
    // immediately. We use selfJID as the voter; the eventual SSE refresh
    // will replace this with the canonical row.
    const optimisticVoters = (detail.votes || []).filter(
      (v) => !(selfJID && voter(v.voter_jid) === voter(selfJID)),
    )
    if (selfJID && next.length > 0) {
      optimisticVoters.push({
        poll_message_id: msg.id,
        poll_chat_jid: msg.chat_jid,
        voter_jid: selfJID,
        selected_options: JSON.stringify(next),
        timestamp: Math.floor(Date.now() / 1000),
      })
    }
    setDetail({ ...detail, votes: optimisticVoters })

    setVoting(true)
    try {
      await api.votePoll(msg.chat_jid, msg.id, next)
    } catch (e) {
      // Roll back on failure.
      setDetail(detail)
      console.warn('poll vote failed:', e)
    } finally {
      setVoting(false)
    }
  }

  if (loadErr) {
    return (
      <div className="mt-1 rounded-lg bg-black/20 px-3 py-2 text-xs text-red-300/80">
        {loadErr}
      </div>
    )
  }
  if (!detail) {
    return (
      <div className="mt-1 rounded-lg bg-black/20 px-3 py-2 text-xs text-neutral-500">
        Loading poll…
      </div>
    )
  }

  const max = Math.max(1, detail.poll.max_selections || 1)
  return (
    <div className={'mt-1 flex flex-col gap-2 rounded-lg bg-black/20 p-3 ' + (mine ? '' : '')}>
      <div className="flex items-baseline justify-between gap-2">
        <div dir="auto" className="text-sm font-medium text-neutral-100">
          {detail.poll.question || 'Poll'}
        </div>
        <span className="shrink-0 text-[10px] uppercase tracking-wider text-neutral-500">
          {max > 1 ? `Pick up to ${max}` : 'Single choice'}
        </span>
      </div>
      <div className="flex flex-col gap-1.5">
        {options.map((opt) => {
          const c = counts.get(opt) || 0
          const pct = totalVoters > 0 ? Math.round((c / totalVoters) * 100) : 0
          const mineHere = myPicks.has(opt)
          return (
            <button
              key={opt}
              onClick={() => cast(opt)}
              disabled={voting}
              className={
                'group relative overflow-hidden rounded-md border px-2.5 py-1.5 text-start text-sm transition disabled:cursor-progress ' +
                (mineHere
                  ? 'border-emerald-500/60 bg-emerald-500/10 text-emerald-100'
                  : 'border-neutral-700 bg-neutral-900/50 text-neutral-200 hover:border-neutral-600')
              }
            >
              {/* Vote bar — fills with the option's share of total voters.
                  Sits behind the row text via absolute positioning so the
                  label stays readable at any percentage. */}
              <div
                className={
                  'absolute inset-y-0 left-0 transition-all ' +
                  (mineHere ? 'bg-emerald-500/20' : 'bg-neutral-700/40')
                }
                style={{ width: pct + '%' }}
                aria-hidden="true"
              />
              <div className="relative flex items-center justify-between gap-3">
                <span dir="auto" className="truncate">{opt}</span>
                <span className="shrink-0 text-[11px] tabular-nums text-neutral-400">
                  {c}{totalVoters > 0 && ` · ${pct}%`}
                </span>
              </div>
            </button>
          )
        })}
      </div>
      <div className="text-[11px] text-neutral-500">
        {totalVoters === 0
          ? 'No votes yet'
          : `${totalVoters} ${totalVoters === 1 ? 'voter' : 'voters'}`}
      </div>
    </div>
  )
}

// voter normalises a JID so we can compare "971…@lid" to "971…@s.whatsapp.net"
// when matching the current user against their own vote row. The bridge can
// write a voter under either form depending on what whatsmeow surfaces.
function voter(jid: string): string {
  return (jid.split('@')[0] || '').split(':')[0]
}

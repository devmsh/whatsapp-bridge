import { useEffect, useState } from 'react'
import { api, type BriefingPayload } from '../api'
import { AwaitingRow, Section, SignalChatRow, TaskRow } from './BriefingView'

// FocusDigest is Focus Mode's digest panel: the same four blocks as the
// global daily briefing (overdue, today, signal chats, awaiting reply) but
// scoped to one circle's flattened chats. Unlike the global briefing, the
// backend regenerates this incrementally (only after >=10 new messages),
// so GET /api/v2/circles/{id}/digest always returns fast — cached data (or
// null) plus a `refreshing` flag — and this component polls again while
// `refreshing` is true.
const POLL_MS = 4000

export function FocusDigest({
  circleId,
  onOpenTask,
  onOpenChat,
}: {
  circleId: number
  onOpenTask: (id: number) => void
  onOpenChat: (jid: string) => void
}) {
  const [digest, setDigest] = useState<BriefingPayload | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // Race guard: a fetch (or a scheduled poll) can resolve after the user
    // has switched circles. `cancelled` is captured by every closure below
    // and checked before every setState; `circleId` itself is also captured
    // by this effect's closure, so a poll scheduled for the previous circle
    // can never apply state for the new one.
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null

    setLoading(true)
    setDigest(null)
    setRefreshing(false)

    function poll() {
      api
        .circleDigest(circleId)
        .then((r) => {
          if (cancelled) return
          // Defense-in-depth: a backend digest with zero matches serializes
          // array fields as JSON null (Go nil-slice), not []. Normalize once
          // here so render code can safely call .length/.map.
          setDigest(
            r.digest
              ? {
                  ...r.digest,
                  today: r.digest.today ?? [],
                  overdue: r.digest.overdue ?? [],
                  signal_chats: r.digest.signal_chats ?? [],
                  awaiting_reply: r.digest.awaiting_reply ?? [],
                }
              : null,
          )
          setRefreshing(r.refreshing)
          setLoading(false)
          if (r.refreshing) {
            timer = setTimeout(() => {
              if (cancelled) return
              poll()
            }, POLL_MS)
          }
        })
        .catch(() => {
          if (cancelled) return
          setLoading(false)
        })
    }

    poll()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [circleId])

  return (
    <div className="flex h-full flex-col overflow-y-auto p-4">
      <div className="mb-2 flex items-baseline justify-between">
        <div className="text-[11px] uppercase tracking-wide text-neutral-500">Digest</div>
        {refreshing && (
          <div className="text-[10px] text-neutral-500">Refreshing…</div>
        )}
      </div>

      {loading && !digest ? (
        <div className="py-10 text-center text-sm text-neutral-600">Loading…</div>
      ) : !digest ? (
        <div className="py-10 text-center text-sm text-neutral-500">
          No digest yet — refreshing…
        </div>
      ) : (
        <div className="flex flex-col gap-5 text-sm">
          {digest.summary && (
            <p dir="auto" className="whitespace-pre-wrap leading-relaxed text-neutral-200">
              {digest.summary}
            </p>
          )}

          {digest.today.length > 0 && (
            <Section title="Do today" hint="High-priority and active open tasks">
              {digest.today.map((t) => (
                <TaskRow key={t.id} t={t} onOpen={() => onOpenTask(t.id)} />
              ))}
            </Section>
          )}

          {digest.overdue.length > 0 && (
            <Section title="Slipping" hint="Past due">
              {digest.overdue.map((t) => (
                <TaskRow key={t.id} t={t} onOpen={() => onOpenTask(t.id)} overdue />
              ))}
            </Section>
          )}

          {digest.signal_chats.length > 0 && (
            <Section title="Signal in this circle" hint="Recent activity that matters">
              {digest.signal_chats.map((c) => (
                <SignalChatRow key={c.jid} c={c} onOpen={() => onOpenChat(c.jid)} />
              ))}
            </Section>
          )}

          {digest.awaiting_reply.length > 0 && (
            <Section title="Awaiting your reply" hint="DMs where they spoke last">
              {digest.awaiting_reply.map((c) => (
                <AwaitingRow key={c.jid} c={c} onOpen={() => onOpenChat(c.jid)} />
              ))}
            </Section>
          )}

          {digest.today.length === 0 &&
            digest.overdue.length === 0 &&
            digest.signal_chats.length === 0 &&
            digest.awaiting_reply.length === 0 && (
              <div className="text-sm text-neutral-500">Nothing pressing in this circle.</div>
            )}

          <div className="pt-1 text-[11px] text-neutral-600">
            {digest.stats_tasks_open} task{digest.stats_tasks_open === 1 ? '' : 's'} open in total.
          </div>
        </div>
      )}
    </div>
  )
}

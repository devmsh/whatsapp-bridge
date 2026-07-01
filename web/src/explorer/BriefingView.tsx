import { useEffect, useState } from 'react'
import {
  api,
  type BriefingAwaiting,
  type BriefingChat,
  type BriefingPayload,
  type BriefingRow,
  type BriefingTask,
} from '../api'
import { jidUser } from './format'

// BriefingModal renders the AI daily briefing: today's tasks, overdue,
// signal-rich chats, and DMs awaiting your reply. The header has a
// "Regenerate" action that runs the briefing sidecar again.
export function BriefingModal({
  onOpenTask,
  onOpenChat,
  onClose,
}: {
  onOpenTask: (id: number) => void
  onOpenChat: (jid: string) => void
  onClose: () => void
}) {
  const [row, setRow] = useState<BriefingRow | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  function load() {
    setLoading(true)
    api
      .briefingToday()
      .then((r) => {
        setRow(r || null)
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }

  useEffect(load, [])

  async function generate() {
    setBusy(true)
    try {
      const fresh = await api.generateBriefing()
      setRow(fresh)
    } catch (e) {
      alert('Briefing failed: ' + (e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  let data: BriefingPayload | null = null
  if (row) {
    try {
      const parsed = JSON.parse(row.data) as BriefingPayload
      // Defense-in-depth: a briefing with zero matches on a field serializes
      // it as JSON null (Go nil-slice), not []. Normalize so render code can
      // safely call .length/.map.
      data = {
        ...parsed,
        today: parsed.today ?? [],
        overdue: parsed.overdue ?? [],
        signal_chats: parsed.signal_chats ?? [],
        awaiting_reply: parsed.awaiting_reply ?? [],
      }
    } catch {}
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="flex h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-5 py-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold">Today</div>
            <div className="text-xs text-neutral-500">
              {row
                ? new Date(row.generated_at * 1000).toLocaleString()
                : 'Daily briefing'}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={generate}
              disabled={busy}
              className="rounded-lg bg-emerald-500/15 px-3 py-1.5 text-xs font-medium text-emerald-300 hover:bg-emerald-500/30 disabled:opacity-50"
            >
              {busy ? 'Generating…' : row ? '↻ Regenerate' : '✨ Generate today’s briefing'}
            </button>
            <button
              onClick={onClose}
              className="rounded-lg border border-neutral-700 px-2.5 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
            >
              Close
            </button>
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {loading ? (
            <div className="py-10 text-center text-sm text-neutral-600">Loading…</div>
          ) : !data ? (
            <div className="py-10 text-center text-sm text-neutral-500">
              No briefing yet for today. Click "Generate today’s briefing" to make one.
            </div>
          ) : (
            <div className="flex flex-col gap-5 text-sm">
              {data.summary && (
                <p dir="auto" className="whitespace-pre-wrap leading-relaxed text-neutral-200">
                  {data.summary}
                </p>
              )}

              {data.today.length > 0 && (
                <Section title="Do today" hint="High-priority and active open tasks">
                  {data.today.map((t) => (
                    <TaskRow key={t.id} t={t} onOpen={() => onOpenTask(t.id)} />
                  ))}
                </Section>
              )}

              {data.overdue.length > 0 && (
                <Section title="Slipping" hint="Past due">
                  {data.overdue.map((t) => (
                    <TaskRow key={t.id} t={t} onOpen={() => onOpenTask(t.id)} overdue />
                  ))}
                </Section>
              )}

              {data.signal_chats.length > 0 && (
                <Section title="Yesterday it mattered" hint="Top signal in the last 24 hours">
                  {data.signal_chats.map((c) => (
                    <SignalChatRow key={c.jid} c={c} onOpen={() => onOpenChat(c.jid)} />
                  ))}
                </Section>
              )}

              {data.awaiting_reply.length > 0 && (
                <Section title="Awaiting your reply" hint="DMs where they spoke last">
                  {data.awaiting_reply.map((c) => (
                    <AwaitingRow key={c.jid} c={c} onOpen={() => onOpenChat(c.jid)} />
                  ))}
                </Section>
              )}

              {data.today.length === 0 &&
                data.overdue.length === 0 &&
                data.signal_chats.length === 0 &&
                data.awaiting_reply.length === 0 && (
                  <div className="text-sm text-neutral-500">Nothing pressing today.</div>
                )}

              <div className="pt-3 text-[11px] text-neutral-600">
                {data.stats_tasks_open} task{data.stats_tasks_open === 1 ? '' : 's'} open in total.
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export function Section({
  title,
  hint,
  children,
}: {
  title: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between">
        <div className="text-[11px] font-semibold uppercase tracking-wide text-neutral-400">
          {title}
        </div>
        {hint && <div className="text-[10px] text-neutral-600">{hint}</div>}
      </div>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

export function TaskRow({
  t,
  overdue,
  onOpen,
}: {
  t: BriefingTask
  overdue?: boolean
  onOpen: () => void
}) {
  return (
    <button
      onClick={onOpen}
      className="flex items-start gap-3 rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-2 text-left hover:bg-neutral-900"
    >
      {t.priority === 'high' && (
        <span className="mt-1 rounded bg-red-500/20 px-1 text-[10px] font-semibold text-red-300">
          HIGH
        </span>
      )}
      <span className="min-w-0 flex-1">
        <span dir="auto" className="block text-sm text-neutral-100">
          {t.title}
        </span>
        <span className="block text-[11px] text-neutral-500">
          {t.assignee || 'unassigned'}
          {t.circle_name ? ' · ' + t.circle_name : ''}
          {t.due_at
            ? ' · due ' + new Date(t.due_at * 1000).toLocaleDateString()
            : ''}
        </span>
      </span>
      {overdue && t.due_at ? (
        <span className="text-[10px] font-semibold text-red-400">
          {daysSince(t.due_at)}d late
        </span>
      ) : null}
    </button>
  )
}

export function SignalChatRow({ c, onOpen }: { c: BriefingChat; onOpen: () => void }) {
  return (
    <button
      onClick={onOpen}
      className="flex flex-col gap-1 rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-2 text-left hover:bg-neutral-900"
    >
      <div className="flex items-center justify-between">
        <span dir="auto" className="truncate text-sm font-medium text-neutral-200">
          {c.name}
        </span>
        <span className="shrink-0 text-[10px] text-neutral-500">
          {c.new_messages} new
        </span>
      </div>
      {c.narrative ? (
        <span dir="auto" className="text-xs text-neutral-400">
          {c.narrative}
        </span>
      ) : null}
    </button>
  )
}

export function AwaitingRow({ c, onOpen }: { c: BriefingAwaiting; onOpen: () => void }) {
  return (
    <button
      onClick={onOpen}
      className="flex flex-col gap-0.5 rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-2 text-left hover:bg-neutral-900"
    >
      <div className="flex items-center justify-between">
        <span dir="auto" className="truncate text-sm text-neutral-200">
          {c.name || '+' + jidUser(c.jid)}
        </span>
        <span className="text-[10px] text-neutral-500">
          {timeAgo(c.last_message_at)}
        </span>
      </div>
      {c.preview ? (
        <span dir="auto" className="truncate text-xs text-neutral-500">
          {c.last_from_name ? c.last_from_name + ': ' : ''}
          {c.preview}
        </span>
      ) : null}
    </button>
  )
}

function daysSince(unix: number) {
  return Math.max(1, Math.floor((Date.now() / 1000 - unix) / 86400))
}

function timeAgo(unix: number) {
  const s = Math.floor(Date.now() / 1000 - unix)
  if (s < 60) return 'just now'
  if (s < 3600) return Math.floor(s / 60) + 'm ago'
  if (s < 86400) return Math.floor(s / 3600) + 'h ago'
  return Math.floor(s / 86400) + 'd ago'
}

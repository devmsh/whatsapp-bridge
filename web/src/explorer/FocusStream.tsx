import { useEffect, useMemo, useState } from 'react'
import { api, type BriefingAwaiting, type BriefingChat, type BriefingPayload, type Chat, type Task } from '../api'
import { isGroup } from './format'
import { ChatAvatar } from './ChatAvatar'

// FocusStream is Focus Mode's entire daily-driver surface: ONE ranked,
// actionable list, replacing what used to be four separate boxes (an AI
// digest with its own mini task lists, a full task board showing the same
// tasks again, a purpose panel, and a plain chat list). The job this screen
// does is triage — "what needs me, ranked" — not "here are four categories
// of data about this circle." Every circle chat is placed in exactly ONE
// section, picked by its single highest-priority reason:
//   overdue task > awaiting-reply DM > due-today task > @mention > signal chat > quiet
function relativeTime(ts?: number): string {
  if (!ts) return ''
  const diff = Date.now() / 1000 - ts
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

function isToday(ts: number): boolean {
  const d = new Date(ts * 1000)
  const now = new Date()
  return (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  )
}

type Reason =
  | { kind: 'task'; task: Task; overdue: boolean }
  | { kind: 'awaiting'; awaiting: BriefingAwaiting }
  | { kind: 'mention' }
  | { kind: 'signal'; signal: BriefingChat }
  | { kind: 'quiet' }

type Row = { chat: Chat; reason: Reason; priority: number }

function priorityOf(reason: Reason): number {
  switch (reason.kind) {
    case 'task':
      return reason.overdue ? 3 : 2
    case 'awaiting':
      return 2.5
    case 'mention':
      return 1
    case 'signal':
      return 0.5
    case 'quiet':
      return 0
  }
}

export function FocusStream({
  circleId,
  chats,
  allTasks,
  nameMap,
  onSelectChat,
  onOpenTask,
  onTasksChanged,
  onBrowseAllTasks,
}: {
  circleId: number
  chats: Chat[]
  allTasks: Task[]
  nameMap: Map<string, string>
  onSelectChat: (jid: string) => void
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onBrowseAllTasks: () => void
}) {
  const [jids, setJids] = useState<Set<string> | null>(null)
  const [digest, setDigest] = useState<BriefingPayload | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [digestLoading, setDigestLoading] = useState(true)
  const [briefExpanded, setBriefExpanded] = useState(false)
  const [quietExpanded, setQuietExpanded] = useState(false)
  const [replyDrafts, setReplyDrafts] = useState<Record<string, string>>({})
  const [sendingTo, setSendingTo] = useState<Set<string>>(new Set())
  const [justReplied, setJustReplied] = useState<Set<string>>(new Set())
  const [justDoneTasks, setJustDoneTasks] = useState<Set<number>>(new Set())

  // Circle chat-jid set (same fetch FocusChatList used).
  useEffect(() => {
    let cancelled = false
    setJids(null)
    api.circleChats(circleId).then((list) => {
      if (!cancelled) setJids(new Set(list))
    })
    return () => {
      cancelled = true
    }
  }, [circleId])

  // Digest fetch + poll-while-refreshing (same pattern FocusDigest used).
  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null

    setDigestLoading(true)
    setDigest(null)
    setRefreshing(false)

    function poll() {
      api
        .circleDigest(circleId)
        .then((r) => {
          if (cancelled) return
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
          setDigestLoading(false)
          // Reconcile the optimistic "just replied" hide-list against fresh
          // server truth on every poll, instead of letting it persist for
          // the rest of the session: once real digest data has arrived, it
          // is more honest than our guess, even if the same jid reappears
          // later as a genuinely new awaiting-reply instance.
          setJustReplied(new Set())
          if (r.refreshing) {
            timer = setTimeout(() => {
              if (cancelled) return
              poll()
            }, 4000)
          }
        })
        .catch(() => {
          if (cancelled) return
          setDigestLoading(false)
        })
    }

    poll()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [circleId])

  // Reset per-circle local optimistic state when switching circles.
  useEffect(() => {
    setJustReplied(new Set())
    setJustDoneTasks(new Set())
    setReplyDrafts({})
    setBriefExpanded(false)
    setQuietExpanded(false)
  }, [circleId])

  // Reconcile the optimistic "just marked done" hide-list against fresh
  // task data whenever the parent's allTasks prop updates (e.g. after
  // onTasksChanged() triggers a refetch), instead of letting a task stay
  // hidden until the user leaves and re-enters the circle — if a task was
  // reopened elsewhere in the app, it should reappear here too.
  useEffect(() => {
    setJustDoneTasks(new Set())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allTasks])

  const { needsYou, moving, quiet, orphanTasks } = useMemo(() => {
    if (!jids) return { needsYou: [] as Row[], moving: [] as Row[], quiet: [] as Row[], orphanTasks: [] as Task[] }

    const circleChats = chats.filter((c) => jids.has(c.jid))
    const circleTasks = allTasks.filter(
      (t) =>
        t.review_status === 'accepted' &&
        t.status !== 'done' &&
        t.status !== 'cancelled' &&
        (t.circle_ids?.includes(circleId) || jids.has(t.origin_chat_jid)),
    )

    const awaitingByJid = new Map((digest?.awaiting_reply ?? []).map((a) => [a.jid, a]))
    const signalByJid = new Map((digest?.signal_chats ?? []).map((s) => [s.jid, s]))

    // Pick each anchorable chat's single best urgent task (preferring overdue
    // over due-today). A task with no chat in this circle to anchor to is an
    // orphan — ALWAYS counted here regardless of urgency, so a non-urgent
    // orphan is never silently invisible: it's still reachable via "browse
    // all tasks", just not forced into the urgency-ranked Needs You section.
    const bestTaskByJid = new Map<string, { task: Task; overdue: boolean }>()
    const orphans: Task[] = []
    for (const t of circleTasks) {
      if (justDoneTasks.has(t.id)) continue
      const overdue = t.due_at > 0 && t.due_at < Date.now() / 1000
      const dueToday = t.due_at > 0 && !overdue && isToday(t.due_at)
      const belongsToChat = t.origin_chat_jid && jids.has(t.origin_chat_jid)
      if (!belongsToChat) {
        orphans.push(t)
        continue
      }
      if (!overdue && !dueToday) continue // anchored but not urgent — visible via the chat itself/full board, not forced into Needs You
      const existing = bestTaskByJid.get(t.origin_chat_jid)
      if (!existing || (overdue && !existing.overdue)) {
        bestTaskByJid.set(t.origin_chat_jid, { task: t, overdue })
      }
    }

    const rows: Row[] = circleChats.map((chat) => {
      let reason: Reason = { kind: 'quiet' }
      let best = 0

      const taskInfo = bestTaskByJid.get(chat.jid)
      if (taskInfo) {
        const r: Reason = { kind: 'task', task: taskInfo.task, overdue: taskInfo.overdue }
        const p = priorityOf(r)
        if (p > best) {
          best = p
          reason = r
        }
      }
      const awaiting = awaitingByJid.get(chat.jid)
      if (awaiting && !justReplied.has(chat.jid)) {
        const r: Reason = { kind: 'awaiting', awaiting }
        const p = priorityOf(r)
        if (p > best) {
          best = p
          reason = r
        }
      }
      if ((chat.unread_mentions ?? 0) > 0) {
        const r: Reason = { kind: 'mention' }
        const p = priorityOf(r)
        if (p > best) {
          best = p
          reason = r
        }
      }
      const signal = signalByJid.get(chat.jid)
      if (signal) {
        const r: Reason = { kind: 'signal', signal }
        const p = priorityOf(r)
        if (p > best) {
          best = p
          reason = r
        }
      }

      return { chat, reason, priority: best }
    })

    rows.sort((a, b) => b.priority - a.priority || b.chat.last_message_at - a.chat.last_message_at)

    return {
      needsYou: rows.filter((r) => r.priority >= 1),
      moving: rows.filter((r) => r.priority === 0.5),
      quiet: rows.filter((r) => r.priority === 0),
      orphanTasks: orphans,
    }
  }, [jids, chats, allTasks, digest, circleId, justReplied, justDoneTasks])

  async function sendReply(jid: string) {
    const text = (replyDrafts[jid] || '').trim()
    if (!text || sendingTo.has(jid)) return
    setSendingTo((s) => new Set(s).add(jid))
    try {
      await api.send(jid, text)
      setReplyDrafts((d) => ({ ...d, [jid]: '' }))
      setJustReplied((s) => new Set(s).add(jid))
    } catch (e) {
      alert('Send failed: ' + (e as Error).message)
    } finally {
      setSendingTo((s) => {
        const n = new Set(s)
        n.delete(jid)
        return n
      })
    }
  }

  async function markTaskDone(task: Task) {
    setJustDoneTasks((s) => new Set(s).add(task.id))
    try {
      await api.updateTask(task.id, { status: 'done' })
      onTasksChanged()
    } catch (e) {
      alert('Failed to mark task done: ' + (e as Error).message)
      setJustDoneTasks((s) => {
        const n = new Set(s)
        n.delete(task.id)
        return n
      })
    }
  }

  if (jids === null) {
    return <div className="p-4 text-xs text-neutral-500">Loading…</div>
  }

  const overdueCount = needsYou.filter((r) => r.reason.kind === 'task' && r.reason.overdue).length
  const pulse =
    `${needsYou.length} need${needsYou.length === 1 ? 's' : ''} you` +
    (overdueCount > 0 ? ` · ${overdueCount} slipping` : '') +
    (digest ? ` · briefed ${relativeTime(digest.generated_at)}` : digestLoading ? ' · briefing…' : '')

  return (
    <div className="flex h-full flex-col overflow-y-auto p-4">
      <div className="mb-3 flex items-baseline justify-between gap-2">
        <div className="text-xs text-neutral-400">{pulse}</div>
        {refreshing && <div className="shrink-0 text-[10px] text-neutral-500">Refreshing…</div>}
      </div>

      {digest?.summary && (
        <div className="mb-4">
          <button
            onClick={() => setBriefExpanded((v) => !v)}
            className="text-[11px] font-medium text-neutral-500 hover:text-neutral-300"
          >
            {briefExpanded ? '▾ hide full briefing' : '▸ full briefing'}
          </button>
          {briefExpanded && (
            <p dir="auto" className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-neutral-300">
              {digest.summary}
            </p>
          )}
        </div>
      )}

      <div className="mb-5">
        {needsYou.length === 0 ? (
          <div className="rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-4 text-center text-sm text-neutral-400">
            You're on top of this circle.
          </div>
        ) : (
          <>
            <div className="mb-1.5 px-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
              Needs you ({needsYou.length})
            </div>
            <div className="space-y-1">
              {needsYou.map((row) => {
                const reason = row.reason
                return (
                  <StreamRow
                    key={row.chat.jid}
                    row={row}
                    title={nameMap.get(row.chat.jid) || row.chat.name}
                    draft={replyDrafts[row.chat.jid] || ''}
                    sending={sendingTo.has(row.chat.jid)}
                    onDraftChange={(v) => setReplyDrafts((d) => ({ ...d, [row.chat.jid]: v }))}
                    onSend={() => sendReply(row.chat.jid)}
                    onMarkDone={reason.kind === 'task' ? () => markTaskDone(reason.task) : undefined}
                    onOpenTaskDetail={reason.kind === 'task' ? () => onOpenTask(reason.task.id) : undefined}
                    onOpen={() => onSelectChat(row.chat.jid)}
                  />
                )
              })}
            </div>
          </>
        )}
        {/* Always reachable, regardless of Needs You being empty — a
            non-urgent orphan task must never be invisible just because
            nothing else needs attention right now. */}
        <button
          onClick={onBrowseAllTasks}
          className="mt-2 px-2 py-1.5 text-xs text-neutral-500 hover:text-neutral-300"
        >
          {orphanTasks.length > 0
            ? `${orphanTasks.length} task${orphanTasks.length === 1 ? '' : 's'} not tied to a chat → See all tasks`
            : 'See all tasks'}
        </button>
      </div>

      {moving.length > 0 && (
        <div className="mb-5">
          <div className="mb-1.5 px-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
            Moving ({moving.length})
          </div>
          <div className="space-y-1">
            {moving.map((row) => (
              <button
                key={row.chat.jid}
                onClick={() => onSelectChat(row.chat.jid)}
                className="flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left hover:bg-neutral-900"
              >
                <ChatAvatar jid={row.chat.jid} title={nameMap.get(row.chat.jid) || row.chat.name} group={isGroup(row.chat.jid)} size={32} />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm font-medium">
                    {nameMap.get(row.chat.jid) || row.chat.name}
                  </div>
                  <div dir="auto" className="truncate text-xs text-neutral-500">
                    {row.reason.kind === 'signal'
                      ? row.reason.signal.narrative || `${row.reason.signal.new_messages} new messages`
                      : ''}
                  </div>
                </div>
                {row.chat.unread_count > 0 && (
                  <span className="shrink-0 rounded-full bg-emerald-500 px-1.5 py-0.5 text-[11px] font-semibold text-neutral-950">
                    {row.chat.unread_count}
                  </span>
                )}
              </button>
            ))}
          </div>
        </div>
      )}

      <div>
        <button
          onClick={() => setQuietExpanded((v) => !v)}
          className="px-1 text-[11px] font-medium text-neutral-500 hover:text-neutral-300"
        >
          {quietExpanded ? '▾' : '▸'} {quiet.length} quiet chat{quiet.length === 1 ? '' : 's'} — Browse all
        </button>
        {quietExpanded && (
          <div className="mt-2 space-y-1">
            {quiet.map((row) => (
              <button
                key={row.chat.jid}
                onClick={() => onSelectChat(row.chat.jid)}
                className="flex w-full items-center gap-3 rounded-lg px-2 py-1.5 text-left hover:bg-neutral-900"
              >
                <ChatAvatar jid={row.chat.jid} title={nameMap.get(row.chat.jid) || row.chat.name} group={isGroup(row.chat.jid)} size={28} />
                <span dir="auto" className="min-w-0 flex-1 truncate text-sm text-neutral-300">
                  {nameMap.get(row.chat.jid) || row.chat.name}
                </span>
                {row.chat.unread_count > 0 && (
                  <span className="shrink-0 rounded-full bg-neutral-700 px-1.5 py-0.5 text-[11px] text-neutral-300">
                    {row.chat.unread_count}
                  </span>
                )}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function StreamRow({
  row,
  title,
  draft,
  sending,
  onDraftChange,
  onSend,
  onMarkDone,
  onOpenTaskDetail,
  onOpen,
}: {
  row: Row
  title: string
  draft: string
  sending: boolean
  onDraftChange: (v: string) => void
  onSend: () => void
  onMarkDone?: () => void
  onOpenTaskDetail?: () => void
  onOpen: () => void
}) {
  const { chat, reason } = row

  return (
    <div className="rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-2.5">
      <div className="flex items-start gap-3">
        <div
          role="button"
          tabIndex={0}
          onClick={onOpen}
          onKeyDown={(e) => e.key === 'Enter' && onOpen()}
          className="flex min-w-0 flex-1 cursor-pointer items-start gap-3 text-left"
        >
          <ChatAvatar jid={chat.jid} title={title} group={isGroup(chat.jid)} size={32} />
          <div className="min-w-0 flex-1">
            <div dir="auto" className="truncate text-sm font-medium">
              {title}
            </div>
            <div dir="auto" className="truncate text-xs text-neutral-500">
              {reason.kind === 'task' && (
                <>
                  <span className={reason.overdue ? 'text-red-400' : 'text-amber-400'}>
                    {reason.overdue ? '⚠ overdue' : '◷ due today'}
                  </span>
                  {' · '}
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      onOpenTaskDetail?.()
                    }}
                    className="underline decoration-dotted hover:text-neutral-300"
                  >
                    {reason.task.title}
                  </button>
                </>
              )}
              {reason.kind === 'awaiting' && (
                <>
                  awaiting reply{reason.awaiting.preview ? ` · "${reason.awaiting.preview}"` : ''}
                </>
              )}
              {reason.kind === 'mention' && '@ you were mentioned'}
              {reason.kind === 'signal' && (reason.signal.narrative || `${reason.signal.new_messages} new messages`)}
            </div>
          </div>
        </div>
        {reason.kind === 'task' && onMarkDone && (
          <button
            onClick={onMarkDone}
            title="Mark task done"
            className="shrink-0 rounded-md border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            ☐ Done
          </button>
        )}
      </div>
      {reason.kind === 'awaiting' && (
        <div className="mt-2 flex gap-2 pl-11">
          <input
            value={draft}
            onChange={(e) => onDraftChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onSend()
            }}
            placeholder="Reply…"
            className="min-w-0 flex-1 rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm outline-none focus:border-neutral-500"
          />
          <button
            onClick={onSend}
            disabled={sending || !draft.trim()}
            className="shrink-0 rounded-md bg-emerald-500 px-3 py-1 text-xs font-medium text-neutral-950 disabled:opacity-50"
          >
            {sending ? 'Sending…' : 'Send'}
          </button>
        </div>
      )}
    </div>
  )
}

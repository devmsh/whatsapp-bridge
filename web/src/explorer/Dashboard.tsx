import { useEffect, useState } from 'react'
import { api, type DashContact, type DashGroup, type Task } from '../api'
import { TagChips } from './Tags'

// DashboardModal: one screen with everything we know about a contact or group:
// profile, tags, circles, open tasks, recent activity, top contributors.
// Opens from the chat header's avatar.
export function DashboardModal({
  kind,
  jid,
  onOpenChat,
  onOpenTask,
  onOpenCircle,
  onClose,
}: {
  kind: 'contact' | 'group'
  jid: string
  onOpenChat: (jid: string) => void
  onOpenTask: (id: number) => void
  onOpenCircle: (id: number) => void
  onClose: () => void
}) {
  const [data, setData] = useState<DashContact | DashGroup | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    const p = kind === 'contact' ? api.contactDashboard(jid) : api.groupDashboard(jid)
    p.then(setData).catch((e) => setErr((e as Error).message))
  }, [kind, jid])

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
            <div dir="auto" className="truncate text-sm font-semibold">
              {data?.name || (kind === 'contact' ? 'Contact' : 'Group')}
            </div>
            <div className="text-xs text-neutral-500">
              {kind === 'group'
                ? `${(data as DashGroup | null)?.participant_count ?? 0} participants`
                : (data as DashContact | null)?.phone || ''}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => {
                onOpenChat(jid)
                onClose()
              }}
              className="rounded-lg bg-emerald-500/15 px-3 py-1.5 text-xs font-medium text-emerald-300 hover:bg-emerald-500/30"
            >
              💬 Open chat
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
          {err && <div className="text-xs text-red-400">Failed: {err}</div>}
          {!data && !err && (
            <div className="py-10 text-center text-sm text-neutral-600">Loading…</div>
          )}
          {data && (
            <div className="flex flex-col gap-5 text-sm">
              {data.profile?.description && (
                <Section title="Purpose">
                  <p dir="auto" className="whitespace-pre-wrap text-neutral-300">
                    {data.profile.description}
                  </p>
                </Section>
              )}

              {kind === 'contact' && (data as DashContact).tags?.length > 0 && (
                <Section title="Tags">
                  <TagChips tags={(data as DashContact).tags} />
                </Section>
              )}

              {data.circles?.length > 0 && (
                <Section title="Circles">
                  <div className="flex flex-wrap gap-1.5">
                    {data.circles.map((c) => (
                      <button
                        key={c.id}
                        onClick={() => {
                          onOpenCircle(c.id)
                          onClose()
                        }}
                        className="rounded-full px-2.5 py-0.5 text-xs font-medium text-neutral-50 hover:opacity-90"
                        style={{ backgroundColor: c.color || '#737373' }}
                      >
                        {c.name}
                      </button>
                    ))}
                  </div>
                </Section>
              )}

              <Section
                title="Open tasks"
                hint={
                  data.tasks_open.length === 0
                    ? `No open tasks. ${data.tasks_done_count} done.`
                    : `${data.tasks_open.length} open · ${data.tasks_done_count} done`
                }
              >
                {data.tasks_open.length > 0 && (
                  <div className="flex flex-col gap-1">
                    {data.tasks_open.map((t) => (
                      <TaskLine
                        key={t.id}
                        t={t}
                        onOpen={() => {
                          onOpenTask(t.id)
                          onClose()
                        }}
                      />
                    ))}
                  </div>
                )}
              </Section>

              {kind === 'group' && (data as DashGroup).top_contributors?.length > 0 && (
                <Section title="Top contributors (last 7 days)">
                  <div className="flex flex-col gap-1">
                    {(data as DashGroup).top_contributors.map((c) => (
                      <div
                        key={c.jid}
                        className="flex items-center justify-between rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-1.5"
                      >
                        <span dir="auto" className="truncate text-sm text-neutral-200">
                          {c.is_admin && <span className="mr-1 text-xs text-amber-300">★</span>}
                          {c.name}
                        </span>
                        <span className="text-xs text-neutral-500">{c.messages} msgs</span>
                      </div>
                    ))}
                  </div>
                </Section>
              )}

              {data.recent?.length > 0 && (
                <Section title="Recent">
                  <div className="flex flex-col gap-1">
                    {data.recent.map((m, i) => (
                      <div
                        key={i}
                        className="rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-1.5"
                      >
                        <div className="flex items-baseline justify-between">
                          <span className="text-xs text-neutral-400">
                            {m.is_from_me ? 'Me' : m.from || 'Someone'}
                          </span>
                          <span className="text-[10px] text-neutral-600">
                            {new Date(m.timestamp * 1000).toLocaleString()}
                          </span>
                        </div>
                        <div
                          dir="auto"
                          className="mt-0.5 truncate text-sm text-neutral-200"
                        >
                          {m.content}
                        </div>
                      </div>
                    ))}
                  </div>
                </Section>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function Section({
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
      <div className="mb-1.5 flex items-baseline justify-between">
        <div className="text-[11px] font-semibold uppercase tracking-wide text-neutral-400">
          {title}
        </div>
        {hint && <div className="text-[10px] text-neutral-600">{hint}</div>}
      </div>
      {children}
    </div>
  )
}

function TaskLine({ t, onOpen }: { t: Task; onOpen: () => void }) {
  return (
    <button
      onClick={onOpen}
      className="flex items-start gap-2 rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-1.5 text-left hover:bg-neutral-900"
    >
      {t.priority === 'high' && (
        <span className="mt-0.5 rounded bg-red-500/20 px-1 text-[10px] font-semibold text-red-300">
          HIGH
        </span>
      )}
      <span dir="auto" className="min-w-0 flex-1 truncate text-sm text-neutral-200">
        {t.title}
      </span>
      <span className="shrink-0 text-[10px] text-neutral-500">{t.status}</span>
    </button>
  )
}

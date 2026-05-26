import { useEffect, useState } from 'react'
import { api, type DashContact } from '../api'
import { ChatAvatar } from './ChatAvatar'
import { DisappearingSection } from './DisappearingSection'

// ContactInfoModal is the focused DM equivalent of GroupInfoModal —
// hero avatar (clickable for full-screen preview), name, phone, business
// badge if applicable, tags, and a single "Open dashboard" footer that
// drops the user into the existing rich Dashboard view when they want
// more (tasks, signal, AI profile, etc.).
//
// The point of this modal isn't to compete with Dashboard; it's the
// fast WA-style "tap the name → who is this" peek. Dashboard stays
// reachable from the avatar and the footer link.
export function ContactInfoModal({
  jid,
  title,
  onClose,
  onOpenDashboard,
}: {
  jid: string
  title: string
  onClose: () => void
  /** Hand off to the full Dashboard view from the footer. */
  onOpenDashboard: () => void
}) {
  const [data, setData] = useState<DashContact | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api.contactDashboard(jid).then((d) => {
      if (!cancelled) setData(d)
    }).catch((e) => {
      if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load contact')
    })
    return () => { cancelled = true }
  }, [jid])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  // Display name + phone fall back to whatever the chat header had if the
  // dashboard fetch hasn't landed (or failed) — never blank.
  const name = data?.name || title
  const phone = data?.phone ? '+' + data.phone : ''
  const businessName = data?.business_name && data.business_name !== name ? data.business_name : ''

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[420px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Contact info</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        {/* Hero card: clickable avatar opens the cycle-12 photo preview,
            name + phone below. Centred so the panel reads as "this is
            the person", same shape as WA's contact-info hero. */}
        <div className="flex flex-col items-center gap-2 border-b border-neutral-800 px-4 py-5">
          <ChatAvatar jid={jid} title={name} size={96} clickable />
          <div className="flex items-center gap-2">
            <div dir="auto" className="text-base font-semibold text-neutral-100">
              {name}
            </div>
            {data?.is_business && (
              <span
                title="WhatsApp Business account"
                className="rounded-full bg-sky-500/20 px-2 py-0.5 text-[10px] uppercase tracking-wider text-sky-200"
              >
                Business
              </span>
            )}
          </div>
          {phone && phone !== name && (
            <div dir="auto" className="text-[12px] text-neutral-400">{phone}</div>
          )}
          {businessName && (
            <div dir="auto" className="text-[12px] text-neutral-500">{businessName}</div>
          )}
        </div>

        <DisappearingSection jid={jid} isGroup={false} />

        <div className="flex-1 overflow-y-auto px-4 py-3">
          {error && <div className="rounded bg-red-500/10 px-3 py-2 text-xs text-red-300">{error}</div>}
          {!error && !data && (
            <div className="py-4 text-center text-xs text-neutral-600">Loading…</div>
          )}

          {data && data.tags.length > 0 && (
            <Section title="Tags">
              <div className="flex flex-wrap gap-1.5">
                {data.tags.map((t) => (
                  <span
                    key={t.id}
                    className="rounded-full px-2 py-0.5 text-[11px] font-medium ring-1"
                    style={{ color: t.color, borderColor: t.color, ['--ring-color' as string]: t.color }}
                  >
                    {t.name}
                  </span>
                ))}
              </div>
            </Section>
          )}

          {data && data.circles.length > 0 && (
            <Section title="Circles">
              <div className="flex flex-wrap gap-1.5">
                {data.circles.map((c) => (
                  <span
                    key={c.id}
                    className="rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ring-neutral-700 text-neutral-300"
                    style={{ color: c.color }}
                  >
                    {c.name}
                  </span>
                ))}
              </div>
            </Section>
          )}

          {data && (
            <Section title="Activity">
              <ul className="grid grid-cols-2 gap-2 text-[12px]">
                <Stat label="Messages" value={data.message_count} />
                <Stat label="Open tasks" value={data.tasks_open.length} />
                <Stat label="Done tasks" value={data.tasks_done_count} />
                <Stat
                  label="Last active"
                  value={data.last_active ? relativeShort(data.last_active) : '—'}
                />
              </ul>
            </Section>
          )}
        </div>

        <footer className="flex items-center justify-between gap-2 border-t border-neutral-800 px-4 py-3">
          <span className="text-[11px] text-neutral-500">
            Need the full picture?
          </span>
          <button
            onClick={onOpenDashboard}
            className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-neutral-950 transition hover:bg-emerald-500"
          >
            Open dashboard →
          </button>
        </footer>
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-3">
      <h3 className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        {title}
      </h3>
      {children}
    </section>
  )
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <li className="rounded-lg bg-neutral-800/50 px-2.5 py-1.5">
      <div className="text-[10px] uppercase tracking-wider text-neutral-500">{label}</div>
      <div className="text-sm font-semibold tabular-nums text-neutral-100">{value}</div>
    </li>
  )
}

function relativeShort(ts: number): string {
  const now = Math.floor(Date.now() / 1000)
  const diff = now - ts
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  if (diff < 86400 * 7) return `${Math.floor(diff / 86400)}d ago`
  return new Date(ts * 1000).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

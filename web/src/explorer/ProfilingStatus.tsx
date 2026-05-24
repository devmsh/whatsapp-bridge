import { useEffect, useState } from 'react'
import { api, type ProfilesStatus } from '../api'

// ProfilingStatus is a small modal: enable the background profiler, watch its
// progress, and trigger a rescan. Profiling writes a short "purpose" for every
// circle, group, and DM — the context the task-extraction agent reads.
export function ProfilingStatusModal({ onClose }: { onClose: () => void }) {
  const [st, setSt] = useState<ProfilesStatus | null>(null)
  const [busy, setBusy] = useState(false)

  function refresh() {
    api.profilesStatus().then(setSt).catch(() => {})
  }

  useEffect(() => {
    refresh()
    const t = window.setInterval(refresh, 3000)
    return () => window.clearInterval(t)
  }, [])

  async function start() {
    setBusy(true)
    try {
      await api.startProfiling()
      setTimeout(refresh, 500)
    } finally {
      setBusy(false)
    }
  }

  const s = st?.stats
  const inFlight = s?.queue_size || 0

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6" onClick={onClose}>
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-950 p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <div className="text-sm font-semibold">Profiles</div>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2.5 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>

        <p className="mb-3 text-xs leading-relaxed text-neutral-400">
          A short "purpose" description is written for every circle, group, and DM. These give the
          task agent the context to tell circles apart and route updates correctly. Refreshed
          automatically every 7 working days.
        </p>

        {st && !st.enabled ? (
          <div className="rounded-xl border border-amber-700/40 bg-amber-500/5 p-3">
            <p className="text-xs text-amber-200/90">
              Profiling is off. Starting it will read your chats with the AI (uses your Max plan).
              This first pass takes a while; silent chats are skipped instantly.
            </p>
            <button
              onClick={start}
              disabled={busy}
              className="mt-2 rounded-lg bg-emerald-500/20 px-3 py-1.5 text-xs font-medium text-emerald-300 hover:bg-emerald-500/30 disabled:opacity-50"
            >
              {busy ? 'Starting…' : 'Start profiling everything'}
            </button>
          </div>
        ) : (
          <div>
            {inFlight > 0 ? (
              <div className="mb-3 rounded-xl border border-sky-700/40 bg-sky-500/5 p-3">
                <div className="flex items-center justify-between text-xs">
                  <span className="font-medium text-sky-200">
                    Generating… ({st?.active.length || 0} in parallel)
                  </span>
                  <span className="text-sky-300">{inFlight} left</span>
                </div>
                {st && st.active.length > 0 && (
                  <div className="mt-1 space-y-0.5">
                    {st.active.map((a) => (
                      <div key={a} className="truncate text-[11px] text-neutral-500">
                        {a}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ) : (
              <div className="mb-3 text-xs text-emerald-300">Up to date — nothing queued.</div>
            )}

            <div className="grid grid-cols-2 gap-2 text-xs">
              <Stat label="Described" value={s?.ok} />
              <Stat label="Empty (stub)" value={s?.empty} />
              <Stat label="Edited by you" value={s?.manual} />
              <Stat label="Stale (will refresh)" value={s?.stale} />
              {(s?.error ?? 0) > 0 && <Stat label="Failed" value={s?.error} danger />}
              <Stat label="Total" value={s?.total} />
            </div>

            <button
              onClick={start}
              disabled={busy}
              className="mt-3 rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-300 hover:bg-neutral-800 disabled:opacity-50"
            >
              {busy ? 'Scanning…' : '↻ Rescan now'}
            </button>
          </div>
        )}
        {!st && <div className="text-xs text-neutral-600">Loading…</div>}
      </div>
    </div>
  )
}

function Stat({ label, value, danger }: { label: string; value?: number; danger?: boolean }) {
  return (
    <div className="rounded-lg border border-neutral-800 bg-neutral-900/40 px-2.5 py-1.5">
      <div className={'text-base font-semibold ' + (danger ? 'text-red-400' : 'text-neutral-200')}>
        {value ?? 0}
      </div>
      <div className="text-[10px] text-neutral-500">{label}</div>
    </div>
  )
}

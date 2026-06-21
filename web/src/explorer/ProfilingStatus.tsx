import { useEffect, useState } from 'react'
import {
  api,
  type AutoExtractStatus,
  type MediaUnderstandingStatus,
  type ProfilesStatus,
} from '../api'

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

            <AutoExtractPanel />
            <MediaUnderstandingPanel />
          </div>
        )}
        {!st && <div className="text-xs text-neutral-600">Loading…</div>}
      </div>
    </div>
  )
}

// AutoExtractPanel toggles continuous circle-level extraction. Idle by default;
// when on, the bridge checks every 10 min for circles that have new messages
// since their last watermark and re-runs incremental extraction on the most-due
// one.
function AutoExtractPanel() {
  const [st, setSt] = useState<AutoExtractStatus | null>(null)

  function reload() {
    api.autoExtractStatus().then(setSt).catch(() => {})
  }
  useEffect(() => {
    reload()
    const t = window.setInterval(reload, 10_000)
    return () => window.clearInterval(t)
  }, [])

  async function toggle() {
    if (!st) return
    const next = await api.autoExtractSet(!st.enabled, undefined)
    setSt(next)
  }
  async function setInterval_(h: number) {
    const next = await api.autoExtractSet(undefined, h)
    setSt(next)
  }

  if (!st) return null
  return (
    <div className="mt-5 rounded-xl border border-neutral-800 bg-neutral-900/40 p-3">
      <div className="mb-1 flex items-center justify-between">
        <div className="text-xs font-semibold text-neutral-200">Auto-extract tasks</div>
        <button
          onClick={toggle}
          className={
            'rounded-full px-2.5 py-0.5 text-[11px] font-medium ' +
            (st.enabled ? 'bg-emerald-500 text-neutral-950' : 'bg-neutral-700 text-neutral-300')
          }
        >
          {st.enabled ? 'ON' : 'OFF'}
        </button>
      </div>
      <p className="text-[11px] text-neutral-500">
        Re-runs circle extraction in the background — only on circles with new messages since their
        last run. Cheap because of the per-chat watermark.
      </p>
      <div className="mt-2 flex items-center gap-2 text-[11px] text-neutral-400">
        <span>Every</span>
        <select
          value={st.interval_hours}
          onChange={(e) => setInterval_(parseInt(e.target.value, 10))}
          className="rounded border border-neutral-700 bg-neutral-900 px-1.5 py-0.5"
        >
          {[1, 2, 4, 8, 12, 24].map((h) => (
            <option key={h} value={h}>
              {h}h
            </option>
          ))}
        </select>
        {st.running && <span className="text-amber-300">Running now…</span>}
      </div>
    </div>
  )
}

// MediaUnderstandingPanel toggles voice transcription (whisper-cli) and image
// description (Claude vision). Voice is gated on a detected binary; if missing,
// shows a one-line install hint instead of a toggle.
function MediaUnderstandingPanel() {
  const [st, setSt] = useState<MediaUnderstandingStatus | null>(null)

  function reload() {
    api.mediaUnderstandingStatus().then(setSt).catch(() => {})
  }
  useEffect(() => {
    reload()
    const t = window.setInterval(reload, 15_000)
    return () => window.clearInterval(t)
  }, [])

  const disabled = !!st?.disabled

  async function toggleAudio() {
    if (!st || disabled) return
    setSt(await api.mediaUnderstandingSet(!st.audio_enabled, undefined))
  }
  async function toggleImage() {
    if (!st || disabled) return
    setSt(await api.mediaUnderstandingSet(undefined, !st.image_enabled))
  }

  if (!st) return null
  const s = st.stats
  return (
    <div className="mt-5 rounded-xl border border-neutral-800 bg-neutral-900/40 p-3">
      <div className="text-xs font-semibold text-neutral-200">Media understanding</div>
      <p className="mt-1 text-[11px] text-neutral-500">
        Transcribe voice notes locally with whisper, and describe images with vision. Enriches what
        the extraction agent sees in noisy chats.
      </p>
      {disabled && (
        <div className="mt-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-amber-300">
          Disabled to protect the API rate limit. The voice-transcript refine pass and image
          description both call the AI, so the feature is paused for now.
        </div>
      )}

      <div className="mt-3 flex items-center justify-between">
        <div>
          <div className="text-xs text-neutral-300">🎙 Voice transcription</div>
          {!st.whisper_detected && (
            <div className="mt-0.5 text-[11px] text-amber-300">
              whisper not detected. Install with{' '}
              <code className="rounded bg-neutral-800 px-1 py-0.5">brew install whisper-cpp</code>{' '}
              then re-open this panel.
            </div>
          )}
          {st.whisper_detected && (
            <div className="mt-0.5 text-[11px] text-neutral-500">
              {s.audio_transcribed} done · {s.audio_pending} pending · {s.audio_error} failed of{' '}
              {s.audio_total}
            </div>
          )}
        </div>
        <button
          onClick={toggleAudio}
          disabled={disabled || !st.whisper_detected}
          className={
            'rounded-full px-2.5 py-0.5 text-[11px] font-medium disabled:opacity-40 ' +
            (st.audio_enabled
              ? 'bg-emerald-500 text-neutral-950'
              : 'bg-neutral-700 text-neutral-300')
          }
        >
          {st.audio_enabled ? 'ON' : 'OFF'}
        </button>
      </div>

      <div className="mt-3 flex items-center justify-between">
        <div>
          <div className="text-xs text-neutral-300">🖼 Image description</div>
          <div className="mt-0.5 text-[11px] text-neutral-500">
            {s.image_described} done · {s.image_pending} pending · {s.image_error} failed of{' '}
            {s.image_total}
          </div>
        </div>
        <button
          onClick={toggleImage}
          disabled={disabled}
          className={
            'rounded-full px-2.5 py-0.5 text-[11px] font-medium disabled:opacity-40 ' +
            (st.image_enabled
              ? 'bg-emerald-500 text-neutral-950'
              : 'bg-neutral-700 text-neutral-300')
          }
        >
          {st.image_enabled ? 'ON' : 'OFF'}
        </button>
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

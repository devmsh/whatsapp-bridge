import { useCallback, useEffect, useState } from 'react'
import {
  api,
  type DebugOverview,
  type DebugService,
  type LLMCall,
  type MeetingScanRow,
  type MeetingScanStatus,
  type ServiceHealth,
  type ServiceRun,
} from '../api'
import { usePoll } from '../hooks/usePoll'

// The Debugging screen: what the background services are doing, and why.
//
// The problem it solves is that almost everything interesting in this app
// happens with nobody watching. Timers fire, models are asked questions, chats
// are read and answers are thrown away — and the only trace used to be a log
// file the LaunchAgent owns and a run list that forgot itself after thirty
// minutes. When something stopped working, there was nothing to look at.
//
// Four tabs, in the order the questions get asked:
//   Services  is anything running, and when does it run next
//   Meetings  the keyword queue, because that scanner is the newest and the
//             one most likely to be doubted
//   Runs      what happened, including last week
//   Model     every prompt sent and every answer, with the text

type Tab = 'services' | 'meetings' | 'runs' | 'model' | 'logs'

const TABS: { id: Tab; label: string }[] = [
  { id: 'services', label: 'Services' },
  { id: 'meetings', label: 'Meeting scanner' },
  { id: 'runs', label: 'Runs' },
  { id: 'model', label: 'Model calls' },
  { id: 'logs', label: 'Log' },
]

export function DebugView() {
  const [tab, setTab] = useState<Tab>('services')

  return (
    <div className="flex h-full min-h-0 flex-col bg-neutral-950">
      <header className="shrink-0 border-b border-neutral-800 px-5 pt-4">
        <h1 className="text-base font-semibold text-neutral-100">Debugging</h1>
        <p className="mt-0.5 text-xs text-neutral-500">
          What the background services are doing, and every question asked of a model.
        </p>
        <nav className="mt-3 flex gap-1 overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={
                'shrink-0 rounded-t-lg border-b-2 px-3 py-2 text-sm transition ' +
                (tab === t.id
                  ? 'border-[#25d366] font-medium text-neutral-100'
                  : 'border-transparent text-neutral-400 hover:text-neutral-200')
              }
            >
              {t.label}
            </button>
          ))}
        </nav>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {tab === 'services' && <ServicesTab />}
        {tab === 'meetings' && <MeetingScanTab />}
        {tab === 'runs' && <RunsTab />}
        {tab === 'model' && <ModelTab />}
        {tab === 'logs' && <LogTab />}
      </div>
    </div>
  )
}

// ── Services ─────────────────────────────────────────────────────────────────

function ServicesTab() {
  const [data, setData] = useState<DebugOverview | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      setData(await api.debugOverview())
      setError('')
    } catch (e) {
      setError(String(e))
    }
  }, [])

  // Ten seconds: fast enough that a run starting is visible, slow enough to
  // cost nothing. usePoll stops the moment the window is hidden.
  usePoll(load, 10_000, [load])

  if (error) return <Problem text={error} />
  if (!data) return <Loading />

  return (
    <div className="space-y-5">
      <div className="grid gap-3 sm:grid-cols-2">
        {data.services.map((s) => (
          <ServiceCard key={s.name} service={s} now={data.now} />
        ))}
      </div>

      <Section title="Engine">
        <dl className="grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
          <Field label="Engine" value={data.engine.engine} />
          <Field label="Model" value={data.engine.model} />
          <Field
            label="Can find meetings"
            value={data.engine.meetings_supported ? 'yes' : 'no'}
            bad={!data.engine.meetings_supported}
          />
          {data.engine.meetings_error && (
            <Field label="Why not" value={data.engine.meetings_error} bad />
          )}
        </dl>
      </Section>

      <Section title="Things the bridge needs">
        <div className="space-y-2">
          {data.dependencies.map((d) => (
            <div key={d.name} className="flex items-start gap-3 text-sm">
              <Dot ok={d.ok} />
              <div className="min-w-0">
                <div className="text-neutral-200">
                  {d.name}
                  <span className="ml-2 text-xs text-neutral-500">{d.what}</span>
                </div>
                <div className="truncate font-mono text-xs text-neutral-600">
                  {d.error ? d.error : (d.url ?? d.path)}
                  {d.models !== undefined && ` · ${d.models} models`}
                </div>
              </div>
            </div>
          ))}
        </div>
      </Section>

      <div className="grid gap-3 sm:grid-cols-2">
        <Section title="Process">
          <dl className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
            <Field label="Uptime" value={duration(data.process.uptime_s)} />
            <Field label="PID" value={String(data.process.pid)} />
            <Field label="Goroutines" value={String(data.process.goroutines)} />
            <Field label="Heap" value={`${data.process.heap_mb} MB`} />
            <Field label="Go" value={data.process.go} />
            <Field label="Host" value={data.process.host} />
          </dl>
        </Section>

        <Section title="Database">
          <dl className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
            {Object.entries(data.database).map(([k, v]) => (
              <Field
                key={k}
                label={k.replace(/_/g, ' ')}
                value={typeof v === 'number' ? v.toLocaleString() : String(v)}
              />
            ))}
          </dl>
        </Section>
      </div>

      <Section title="Model call log">
        <dl className="grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-3">
          <Field label="Calls kept" value={data.llm_log.rows.toLocaleString()} />
          <Field label="Text stored" value={bytes(data.llm_log.bytes)} />
          <Field label="Failed" value={String(data.llm_log.failed)} bad={data.llm_log.failed > 0} />
        </dl>
        <p className="mt-2 text-xs text-neutral-600">
          Prompts and answers are kept for 14 days, then deleted.
        </p>
      </Section>
    </div>
  )
}

function ServiceCard({ service, now }: { service: DebugService; now: number }) {
  const s = service
  return (
    <div className="rounded-xl border border-neutral-800 bg-neutral-900/40 p-4">
      <div className="flex items-start gap-2.5">
        <Health health={s.health} />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <h3 className="text-sm font-semibold text-neutral-100">{s.name}</h3>
            {s.running && <span className="text-xs text-[#25d366]">running now</span>}
          </div>
          <p className="mt-0.5 text-xs leading-relaxed text-neutral-500">{s.does}</p>
        </div>
      </div>

      <div className="mt-3 space-y-1 text-xs">
        <Row label="Schedule" value={s.schedule} />
        {s.last_ticked_at ? (
          <Row label="Last checked" value={ago(s.last_ticked_at, now)} />
        ) : (
          <Row label="Last checked" value="not yet" />
        )}
        {s.next_tick_at && !s.running && (
          <Row label="Next check" value={until(s.next_tick_at, now)} />
        )}
        {s.detail &&
          Object.entries(s.detail).map(([k, v]) => (
            <Row key={k} label={k.replace(/_/g, ' ')} value={renderValue(v)} />
          ))}
      </div>

      {s.note && (
        <p className="mt-3 rounded-lg bg-neutral-800/60 px-2.5 py-1.5 text-xs text-neutral-400">
          {s.note}
        </p>
      )}
    </div>
  )
}

// ── Meeting scanner ──────────────────────────────────────────────────────────

function MeetingScanTab() {
  const [status, setStatus] = useState<MeetingScanStatus | null>(null)
  const [queue, setQueue] = useState<MeetingScanRow[]>([])
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const r = await api.meetingScan()
      setStatus(r.status)
      setQueue(r.queue)
      setError('')
    } catch (e) {
      setError(String(e))
    }
  }, [])

  usePoll(load, 10_000, [load])

  async function patch(body: Parameters<typeof api.setMeetingScan>[0], label = '') {
    setBusy(label || 'saving')
    try {
      const r = await api.setMeetingScan(body)
      setStatus(r.status)
      await load()
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy('')
    }
  }

  if (error && !status) return <Problem text={error} />
  if (!status) return <Loading />

  return (
    <div className="space-y-5">
      <Section title="How it works">
        <ol className="space-y-1.5 text-sm text-neutral-400">
          <li>
            <b className="text-neutral-200">1.</b> Every {Math.round(status.tick_seconds / 60)}{' '}
            minutes, new messages are checked for meeting words. No model, a few milliseconds.
          </li>
          <li>
            <b className="text-neutral-200">2.</b> A chat with meeting talk is read by the local
            model — one chat at a time, and not the same chat twice within{' '}
            {status.cooldown_hours} hours.
          </li>
          <li>
            <b className="text-neutral-200">3.</b> Whatever it finds waits for you to accept it in
            Meetings. Nothing is added to your calendar on its own.
          </li>
        </ol>
      </Section>

      <Section title="Settings">
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2 text-sm text-neutral-200">
            <input
              type="checkbox"
              checked={status.enabled}
              onChange={(e) => patch({ enabled: e.target.checked })}
              className="h-4 w-4 accent-[#25d366]"
            />
            Find new meetings automatically
          </label>

          <label className="flex items-center gap-2 text-sm text-neutral-400">
            Read one chat at most every
            <input
              type="number"
              min={1}
              max={72}
              value={status.cooldown_hours}
              onChange={(e) => patch({ cooldown_hours: Number(e.target.value) })}
              className="w-16 rounded-lg border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm text-neutral-100"
            />
            hours
          </label>

          <button
            onClick={() => patch({ run_now: true }, 'tick')}
            disabled={!!busy || status.running}
            className="rounded-lg border border-neutral-700 px-3 py-1.5 text-sm text-neutral-200 transition hover:bg-neutral-800 disabled:opacity-40"
          >
            {busy === 'tick' ? 'Checking…' : 'Check now'}
          </button>
        </div>

        <dl className="mt-4 grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-4">
          <Field label="Running" value={status.running ? 'yes' : 'no'} />
          <Field label="Chats waiting" value={String(status.waiting)} />
          <Field label="Scanned last pass" value={String(status.last_scanned)} />
          <Field label="Flagged last pass" value={String(status.last_flagged)} />
        </dl>
      </Section>

      <Section title={`Queue (${queue.length})`}>
        {queue.length === 0 ? (
          <Empty text="No chat has been scanned yet. The first pass runs a minute or two after the bridge starts." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-sm">
              <thead>
                <tr className="border-b border-neutral-800 text-left text-xs text-neutral-500">
                  <Th>Chat</Th>
                  <Th>Waiting</Th>
                  <Th>Last meeting talk</Th>
                  <Th>Last read by model</Th>
                  <Th>Found</Th>
                  <Th />
                </tr>
              </thead>
              <tbody>
                {queue.map((row) => (
                  <tr key={row.chat_jid} className="border-b border-neutral-900/70">
                    <Td>
                      <span dir="auto" className="text-neutral-200">
                        {row.chat_name}
                      </span>
                    </Td>
                    <Td>
                      {row.hits > 0 ? (
                        <span className="rounded-full bg-[#25d366]/15 px-2 py-0.5 text-xs font-medium text-[#25d366]">
                          {row.hits}
                        </span>
                      ) : (
                        <span className="text-neutral-600">—</span>
                      )}
                    </Td>
                    <Td>{row.last_hit_ts ? when(row.last_hit_ts) : '—'}</Td>
                    <Td>{row.last_run_at ? when(row.last_run_at) : 'never'}</Td>
                    <Td>{row.found || '—'}</Td>
                    <Td>
                      <button
                        onClick={() => patch({ run_now: true, chat_jid: row.chat_jid }, row.chat_jid)}
                        disabled={!!busy || status.running}
                        className="rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 transition hover:bg-neutral-800 disabled:opacity-40"
                      >
                        {busy === row.chat_jid ? 'Starting…' : 'Read now'}
                      </button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>

      {error && <Problem text={error} />}
    </div>
  )
}

// ── Runs ─────────────────────────────────────────────────────────────────────

function RunsTab() {
  const [runs, setRuns] = useState<ServiceRun[] | null>(null)
  const [service, setService] = useState('')
  const [open, setOpen] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setRuns(await api.debugRuns({ service: service || undefined, limit: 100 }))
    } catch {
      setRuns([])
    }
  }, [service])

  usePoll(load, 15_000, [load])

  if (!runs) return <Loading />

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1">
        {['', 'tasks', 'meetings', 'profiles', 'digest'].map((s) => (
          <button
            key={s || 'all'}
            onClick={() => setService(s)}
            className={
              'rounded-full px-3 py-1 text-xs transition ' +
              (service === s
                ? 'bg-neutral-700 text-neutral-100'
                : 'bg-neutral-900 text-neutral-400 hover:text-neutral-200')
            }
          >
            {s || 'everything'}
          </button>
        ))}
      </div>

      {runs.length === 0 ? (
        <Empty text="No runs recorded yet." />
      ) : (
        <div className="overflow-hidden rounded-xl border border-neutral-800">
          {runs.map((r) => (
            <RunRow key={r.id} run={r} open={open === r.id} onToggle={() => setOpen(open === r.id ? null : r.id)} />
          ))}
        </div>
      )}
    </div>
  )
}

function RunRow({ run, open, onToggle }: { run: ServiceRun; open: boolean; onToggle: () => void }) {
  const [detail, setDetail] = useState<{ run: ServiceRun; calls: LLMCall[] } | null>(null)

  useEffect(() => {
    if (!open || detail) return
    api.debugRun(run.id).then(setDetail).catch(() => undefined)
  }, [open, detail, run.id])

  return (
    <div className="border-b border-neutral-900 last:border-b-0">
      <button
        onClick={onToggle}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition hover:bg-neutral-900/60"
      >
        <StatusPill status={run.status} />
        <div className="min-w-0 flex-1">
          <div dir="auto" className="truncate text-sm text-neutral-200">
            {run.label || run.subject}
          </div>
          <div className="truncate text-xs text-neutral-500">
            {run.service} · {run.trigger} · {when(run.started_at)}
            {run.ended_at ? ` · took ${duration(run.ended_at - run.started_at)}` : ''}
            {run.created > 0 ? ` · made ${run.created}` : ''}
          </div>
        </div>
        <span className="shrink-0 text-xs text-neutral-600">{open ? '−' : '+'}</span>
      </button>

      {open && (
        <div className="space-y-3 bg-neutral-900/40 px-4 py-3">
          {run.summary && <p className="text-xs text-neutral-300">{run.summary}</p>}
          {run.error && (
            <p className="rounded-lg bg-red-500/10 px-2.5 py-1.5 text-xs text-red-300">{run.error}</p>
          )}
          {!detail ? (
            <p className="text-xs text-neutral-600">Loading…</p>
          ) : (
            <>
              {detail.run.events && detail.run.events.length > 0 && (
                <div>
                  <h4 className="mb-1 text-xs font-medium text-neutral-400">Progress</h4>
                  <pre className="max-h-64 overflow-auto rounded-lg bg-neutral-950 p-2.5 font-mono text-[11px] leading-relaxed text-neutral-400">
                    {detail.run.events.map((e) => `${e.name ?? e.kind}: ${e.text ?? ''}`).join('\n')}
                  </pre>
                </div>
              )}
              <div>
                <h4 className="mb-1 text-xs font-medium text-neutral-400">
                  Model calls ({detail.calls.length})
                </h4>
                {detail.calls.length === 0 ? (
                  <p className="text-xs text-neutral-600">None recorded.</p>
                ) : (
                  <CallList calls={detail.calls} />
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

// ── Model calls ──────────────────────────────────────────────────────────────

function ModelTab() {
  const [calls, setCalls] = useState<LLMCall[] | null>(null)
  const [service, setService] = useState('')
  const [failedOnly, setFailedOnly] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await api.debugLLM({
        service: service || undefined,
        failed: failedOnly || undefined,
        limit: 150,
      })
      setCalls(r.calls)
    } catch {
      setCalls([])
    }
  }, [service, failedOnly])

  usePoll(load, 15_000, [load])

  if (!calls) return <Loading />

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex flex-wrap gap-1">
          {['', 'tasks', 'meetings', 'media'].map((s) => (
            <button
              key={s || 'all'}
              onClick={() => setService(s)}
              className={
                'rounded-full px-3 py-1 text-xs transition ' +
                (service === s
                  ? 'bg-neutral-700 text-neutral-100'
                  : 'bg-neutral-900 text-neutral-400 hover:text-neutral-200')
              }
            >
              {s || 'everything'}
            </button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-xs text-neutral-400">
          <input
            type="checkbox"
            checked={failedOnly}
            onChange={(e) => setFailedOnly(e.target.checked)}
            className="h-3.5 w-3.5 accent-[#25d366]"
          />
          only the ones that failed
        </label>
      </div>

      {calls.length === 0 ? (
        <Empty text="No model calls recorded yet." />
      ) : (
        <CallList calls={calls} />
      )}
    </div>
  )
}

function CallList({ calls }: { calls: LLMCall[] }) {
  const [open, setOpen] = useState<number | null>(null)
  return (
    <div className="overflow-hidden rounded-xl border border-neutral-800">
      {calls.map((c) => (
        <CallRow key={c.id} call={c} open={open === c.id} onToggle={() => setOpen(open === c.id ? null : c.id)} />
      ))}
    </div>
  )
}

function CallRow({ call, open, onToggle }: { call: LLMCall; open: boolean; onToggle: () => void }) {
  const [full, setFull] = useState<LLMCall | null>(null)

  useEffect(() => {
    if (!open || full) return
    api.debugLLMCall(call.id).then(setFull).catch(() => undefined)
  }, [open, full, call.id])

  return (
    <div className="border-b border-neutral-900 last:border-b-0">
      <button
        onClick={onToggle}
        className="flex w-full items-center gap-3 px-4 py-2 text-left transition hover:bg-neutral-900/60"
      >
        <Dot ok={call.ok} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm text-neutral-200">
            <span className="font-medium">{call.kind}</span>
            <span className="ml-2 text-xs text-neutral-500">
              {call.engine}:{call.model}
            </span>
            {call.attempt > 1 && (
              <span className="ml-2 text-xs text-amber-400">retry {call.attempt}</span>
            )}
          </div>
          <div className="truncate text-xs text-neutral-500">
            {call.service} · {when(call.created_at)} · {(call.latency_ms / 1000).toFixed(1)}s ·{' '}
            {bytes(call.prompt_chars)} in, {bytes(call.reply_chars)} out
            {call.error ? ` · ${call.error}` : ''}
          </div>
        </div>
        <span className="shrink-0 text-xs text-neutral-600">{open ? '−' : '+'}</span>
      </button>

      {open && (
        <div className="space-y-3 bg-neutral-900/40 px-4 py-3">
          {!full ? (
            <p className="text-xs text-neutral-600">Loading…</p>
          ) : (
            <>
              {full.error && (
                <p className="rounded-lg bg-red-500/10 px-2.5 py-1.5 text-xs text-red-300">
                  {full.error}
                </p>
              )}
              <Blob title="System prompt" text={full.system_prompt} />
              <Blob title="Sent" text={full.prompt} />
              <Blob title="Answer" text={full.response} />
            </>
          )}
        </div>
      )}
    </div>
  )
}

function Blob({ title, text }: { title: string; text?: string }) {
  const [expanded, setExpanded] = useState(false)
  if (!text) return null
  const long = text.length > 1200
  const shown = expanded || !long ? text : text.slice(0, 1200)
  return (
    <div>
      <div className="mb-1 flex items-baseline gap-2">
        <h4 className="text-xs font-medium text-neutral-400">{title}</h4>
        <span className="text-[11px] text-neutral-600">{bytes(text.length)}</span>
        {long && (
          <button
            onClick={() => setExpanded(!expanded)}
            className="text-[11px] text-neutral-500 underline hover:text-neutral-300"
          >
            {expanded ? 'show less' : 'show all'}
          </button>
        )}
      </div>
      <pre
        dir="auto"
        className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-neutral-950 p-2.5 font-mono text-[11px] leading-relaxed text-neutral-300"
      >
        {shown}
        {long && !expanded ? '\n…' : ''}
      </pre>
    </div>
  )
}

// ── Log ──────────────────────────────────────────────────────────────────────

function LogTab() {
  const [data, setData] = useState<{ path: string; lines: string[]; note?: string } | null>(null)

  const load = useCallback(async () => {
    try {
      setData(await api.debugLogs(300))
    } catch (e) {
      setData({ path: '', lines: [String(e)] })
    }
  }, [])

  usePoll(load, 15_000, [load])

  if (!data) return <Loading />
  if (data.note) return <Empty text={data.note} />

  return (
    <div className="space-y-2">
      <p className="font-mono text-xs text-neutral-600">{data.path}</p>
      <pre className="max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-xl border border-neutral-800 bg-neutral-950 p-3 font-mono text-[11px] leading-relaxed text-neutral-400">
        {data.lines.join('\n')}
      </pre>
    </div>
  )
}

// ── Small shared pieces ──────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-xl border border-neutral-800 bg-neutral-900/40 p-4">
      <h2 className="mb-3 text-sm font-semibold text-neutral-200">{title}</h2>
      {children}
    </section>
  )
}

function Field({ label, value, bad }: { label: string; value: string; bad?: boolean }) {
  return (
    <div className="flex min-w-0 justify-between gap-3">
      <dt className="shrink-0 text-neutral-500">{label}</dt>
      <dd className={'truncate text-right ' + (bad ? 'text-red-400' : 'text-neutral-200')}>
        {value}
      </dd>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3">
      <span className="shrink-0 text-neutral-500">{label}</span>
      <span className="truncate text-right text-neutral-300">{value}</span>
    </div>
  )
}

const HEALTH_STYLE: Record<ServiceHealth, { colour: string; label: string }> = {
  ok: { colour: 'bg-[#25d366]', label: 'working' },
  idle: { colour: 'bg-neutral-500', label: 'waiting for its next turn' },
  off: { colour: 'bg-neutral-700', label: 'switched off' },
  broken: { colour: 'bg-red-500', label: 'has not run when it should have' },
}

function Health({ health }: { health: ServiceHealth }) {
  const s = HEALTH_STYLE[health] ?? HEALTH_STYLE.idle
  return <span className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${s.colour}`} title={s.label} />
}

function Dot({ ok }: { ok: boolean }) {
  return (
    <span
      className={'mt-1.5 h-2 w-2 shrink-0 rounded-full ' + (ok ? 'bg-[#25d366]' : 'bg-red-500')}
    />
  )
}

function StatusPill({ status }: { status: string }) {
  const tone =
    status === 'done'
      ? 'bg-[#25d366]/15 text-[#25d366]'
      : status === 'running' || status === 'starting'
        ? 'bg-sky-500/15 text-sky-300'
        : status === 'interrupted'
          ? 'bg-amber-500/15 text-amber-300'
          : 'bg-red-500/15 text-red-300'
  return (
    <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${tone}`}>
      {status}
    </span>
  )
}

function Th({ children }: { children?: React.ReactNode }) {
  return <th className="px-3 py-2 font-medium">{children}</th>
}

function Td({ children }: { children?: React.ReactNode }) {
  return <td className="px-3 py-2 text-neutral-400">{children}</td>
}

function Loading() {
  return <p className="text-sm text-neutral-600">Loading…</p>
}

function Empty({ text }: { text: string }) {
  return <p className="text-sm text-neutral-600">{text}</p>
}

function Problem({ text }: { text: string }) {
  return (
    <p className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">{text}</p>
  )
}

function renderValue(v: unknown): string {
  if (typeof v === 'boolean') return v ? 'yes' : 'no'
  if (typeof v === 'number') return v.toLocaleString()
  return String(v)
}

function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

function duration(seconds: number): string {
  if (seconds < 60) return `${Math.max(0, Math.round(seconds))}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`
  return `${Math.round(seconds / 86400)}d`
}

function ago(ts: number, now: number): string {
  const d = now - ts
  if (d < 60) return 'just now'
  return `${duration(d)} ago`
}

function until(ts: number, now: number): string {
  const d = ts - now
  if (d <= 0) return 'any moment'
  return `in ${duration(d)}`
}

function when(ts: number): string {
  const d = new Date(ts * 1000)
  const today = new Date()
  const sameDay =
    d.getDate() === today.getDate() &&
    d.getMonth() === today.getMonth() &&
    d.getFullYear() === today.getFullYear()
  return sameDay
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

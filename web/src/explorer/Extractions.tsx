import { useEffect, useRef, useState } from 'react'
import {
  api,
  type ExtractionRun,
  type ExtractionRunEvent,
  type ExtractionRunState,
  type ExtractionStep,
} from '../api'

// ExtractionsModal shows extraction-run state for a chat or circle.
// - If `liveRunId` is provided, it shows the LIVE run (SSE, with cancel).
//   Once that run finishes it switches to showing its session transcript.
// - Otherwise it lists past runs and lets you open any one's transcript.
export function ExtractionsModal({
  title,
  fetchRuns,
  liveRunId,
  onClose,
}: {
  title: string
  fetchRuns: () => Promise<ExtractionRun[]>
  liveRunId?: string | null
  onClose: () => void
}) {
  const [runs, setRuns] = useState<ExtractionRun[] | null>(null)
  const [selected, setSelected] = useState<string | null>(null)

  useEffect(() => {
    fetchRuns().then(setRuns).catch(() => setRuns([]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="flex h-[80vh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-5 py-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold">Extraction history</div>
            <div dir="auto" className="truncate text-xs text-neutral-500">
              {title}
            </div>
          </div>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2.5 py-1 text-xs text-neutral-300 transition hover:bg-neutral-800"
          >
            Close
          </button>
        </header>

        <div className="flex min-h-0 flex-1">
          <div className="w-72 shrink-0 overflow-y-auto border-r border-neutral-800">
            {runs === null ? (
              <div className="p-4 text-sm text-neutral-600">Loading…</div>
            ) : runs.length === 0 ? (
              <div className="p-4 text-sm text-neutral-600">No runs yet.</div>
            ) : (
              runs.map((r) => (
                <button
                  key={r.session_id}
                  onClick={() => setSelected(r.session_id)}
                  className={
                    'block w-full border-b border-neutral-900 px-4 py-3 text-left transition hover:bg-neutral-900 ' +
                    (selected === r.session_id ? 'bg-neutral-900' : '')
                  }
                >
                  <div className="truncate text-sm text-neutral-200">{r.title}</div>
                  <div className="mt-0.5 text-[11px] text-neutral-500">
                    {r.last_modified ? new Date(r.last_modified).toLocaleString() : ''}
                  </div>
                </button>
              ))
            )}
          </div>

          <div className="min-w-0 flex-1 overflow-y-auto">
            {liveRunId ? (
              <LiveRun runId={liveRunId} onSessionReady={(sid) => setSelected(sid)} />
            ) : selected ? (
              <Transcript sessionId={selected} />
            ) : (
              <div className="p-6 text-sm text-neutral-600">
                Select a run to see what the agent did.
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

// Strip the MCP prefix so tool names read cleanly (mcp__whatsapp__wa_scan → wa_scan).
function cleanTool(name?: string) {
  return (name || '').replace(/^mcp__[^_]+__/, '')
}

function Transcript({ sessionId }: { sessionId: string }) {
  const [steps, setSteps] = useState<ExtractionStep[] | null>(null)

  useEffect(() => {
    setSteps(null)
    api.extractionTranscript(sessionId).then(setSteps).catch(() => setSteps([]))
  }, [sessionId])

  if (steps === null) return <div className="p-6 text-sm text-neutral-600">Loading transcript…</div>
  if (steps.length === 0)
    return <div className="p-6 text-sm text-neutral-600">No transcript available.</div>

  return (
    <div className="flex flex-col gap-3 p-5">
      {steps.map((s, i) => (
        <Step key={i} step={s} />
      ))}
    </div>
  )
}

function Step({ step }: { step: ExtractionStep }) {
  if (step.type === 'assistant_text' || step.type === 'text') {
    return (
      <div dir="auto" className="whitespace-pre-wrap text-sm leading-relaxed text-neutral-200">
        {step.text}
      </div>
    )
  }

  if (step.type === 'tool_use') {
    return (
      <div className="rounded-lg border border-sky-900/60 bg-sky-950/30 px-3 py-2">
        <div className="text-xs font-semibold text-sky-300">→ {cleanTool(step.name)}</div>
        {step.input != null && (
          <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-words text-[11px] text-neutral-400">
            {JSON.stringify(step.input, null, 2)}
          </pre>
        )}
      </div>
    )
  }

  // tool_result
  return (
    <ToolResult step={step} />
  )
}

// LiveRun streams an in-progress extraction over SSE: state, every tool call,
// agent text lines, and a final result/error. When the run completes and a
// session_id is known, it calls onSessionReady so the parent can flip to the
// historical transcript view (richer detail than the live stream).
function LiveRun({
  runId,
  onSessionReady,
}: {
  runId: string
  onSessionReady: (sessionId: string) => void
}) {
  const [state, setState] = useState<Partial<ExtractionRunState>>({})
  const [events, setEvents] = useState<ExtractionRunEvent[]>([])
  const [cancelling, setCancelling] = useState(false)
  const sessionFiredRef = useRef(false)
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const es = new EventSource(api.runStreamURL(runId))
    es.addEventListener('state', (ev: MessageEvent) => {
      try {
        const next = JSON.parse(ev.data) as Partial<ExtractionRunState>
        setState((prev) => ({ ...prev, ...next }))
        if (next.session_id && !sessionFiredRef.current) {
          sessionFiredRef.current = true
          // Defer slightly so we don't yank the user away mid-stream.
          const terminal = next.status === 'done' || next.status === 'failed' || next.status === 'cancelled'
          if (terminal) setTimeout(() => onSessionReady(next.session_id!), 600)
        }
      } catch {}
    })
    es.addEventListener('event', (ev: MessageEvent) => {
      try {
        const e = JSON.parse(ev.data) as ExtractionRunEvent
        setEvents((prev) => [...prev, e])
      } catch {}
    })
    es.addEventListener('end', () => es.close())
    es.onerror = () => es.close()
    return () => es.close()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [events.length])

  async function cancel() {
    setCancelling(true)
    try { await api.cancelRun(runId) } finally { setCancelling(false) }
  }

  const live = state.status === 'starting' || state.status === 'running'
  const toolCount = events.filter((e) => e.kind === 'tool').length

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-3 border-b border-neutral-800 px-5 py-3 text-xs">
        <span
          className={
            'inline-flex h-2 w-2 rounded-full ' +
            (live ? 'animate-pulse bg-emerald-400' : state.status === 'done' ? 'bg-emerald-500' : state.status === 'cancelled' ? 'bg-amber-500' : 'bg-red-500')
          }
        />
        <span className="font-medium text-neutral-200">
          {state.status === 'done'
            ? 'Done'
            : state.status === 'cancelled'
              ? 'Cancelled'
              : state.status === 'failed'
                ? 'Failed'
                : 'Running…'}
        </span>
        <span className="text-neutral-500">
          {toolCount} tool calls · {state.created ?? 0} task{(state.created ?? 0) === 1 ? '' : 's'} created
        </span>
        <div className="ml-auto flex gap-2">
          {live && (
            <button
              onClick={cancel}
              disabled={cancelling}
              className="rounded border border-red-800 px-2 py-1 text-[11px] text-red-300 hover:bg-red-900/30 disabled:opacity-50"
            >
              {cancelling ? 'Cancelling…' : '✕ Cancel'}
            </button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {events.length === 0 ? (
          <div className="text-sm text-neutral-600">Waiting for the agent to start…</div>
        ) : (
          <div className="flex flex-col gap-2">
            {events.map((e) => (
              <LiveEvent key={e.seq} ev={e} />
            ))}
            <div ref={bottomRef} />
          </div>
        )}
        {state.summary && !live && (
          <div className="mt-4 rounded-lg border border-neutral-800 bg-neutral-900/60 p-3">
            <div className="text-[11px] uppercase tracking-wide text-neutral-500">Summary</div>
            <div dir="auto" className="mt-1 whitespace-pre-wrap text-xs text-neutral-200">
              {state.summary}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function LiveEvent({ ev }: { ev: ExtractionRunEvent }) {
  if (ev.kind === 'tool') {
    return (
      <div className="rounded-md border border-sky-900/60 bg-sky-950/30 px-3 py-1.5 text-xs">
        <span className="font-semibold text-sky-300">→ {ev.name}</span>
      </div>
    )
  }
  if (ev.kind === 'error') {
    return <div className="text-xs text-red-400">{ev.text}</div>
  }
  return (
    <div dir="auto" className="whitespace-pre-wrap text-xs leading-relaxed text-neutral-300">
      {ev.text}
    </div>
  )
}

function ToolResult({ step }: { step: ExtractionStep }) {
  const [open, setOpen] = useState(false)
  const content = step.content || ''
  const preview = content.length > 240 ? content.slice(0, 240) + '…' : content
  const long = content.length > 240
  return (
    <div
      className={
        'rounded-lg border px-3 py-2 ' +
        (step.is_error ? 'border-red-900/60 bg-red-950/30' : 'border-neutral-800 bg-neutral-900/40')
      }
    >
      <div className="flex items-center justify-between">
        <div className={'text-xs font-semibold ' + (step.is_error ? 'text-red-300' : 'text-neutral-400')}>
          {step.is_error ? '✗' : '←'} {cleanTool(step.name) || 'result'}
        </div>
        {long && (
          <button
            onClick={() => setOpen((o) => !o)}
            className="text-[11px] text-neutral-500 hover:text-neutral-300"
          >
            {open ? 'Show less' : 'Show all'}
          </button>
        )}
      </div>
      <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-words text-[11px] text-neutral-400">
        {open ? content : preview}
      </pre>
    </div>
  )
}

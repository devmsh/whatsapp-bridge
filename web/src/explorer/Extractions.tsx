import { useEffect, useState } from 'react'
import { api, type ExtractionRun, type ExtractionStep } from '../api'

// ExtractionsModal shows the history of AI task-extraction runs for one chat,
// read straight from the Claude Agent SDK session store (no app DB). Picking a
// run loads its full transcript: the agent's reasoning, every MCP tool call
// with its arguments, and each tool's response.
export function ExtractionsModal({
  title,
  fetchRuns,
  onClose,
}: {
  title: string
  fetchRuns: () => Promise<ExtractionRun[]>
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
            {selected ? (
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

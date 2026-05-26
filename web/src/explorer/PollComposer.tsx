import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

// PollComposer is the modal that WA pops when you tap the Poll attachment.
// Three sections in one column:
//   1. Question — single line input, autofocused.
//   2. Options — dynamic list. We start with 2 empty rows (WA's minimum),
//      auto-add a fresh blank row whenever the user fills the last one,
//      and cap at 12 (WA's hard limit). Each row has a ✕ to remove (only
//      when there are more than 2 — never let the user drop below the
//      minimum).
//   3. "Allow multiple answers" toggle — controls max_selections (1 vs
//      options.length). WA exposes this exact toggle on the poll-create
//      sheet.
//
// Send pings the bridge's POST /api/v2/polls which whatsmeow handles via
// BuildPollCreation (the MessageSecret matters — without it the poll
// can't be voted on by other devices). On success we echo a poll bubble
// locally so the user sees it land instantly; the SSE round-trip will
// replace it with the canonical row.
export function PollComposer({
  jid,
  onClose,
  onSent,
}: {
  jid: string
  onClose: () => void
  onSent: (messageID: string, question: string, options: string[], maxSelections: number) => void
}) {
  const [question, setQuestion] = useState('')
  const [options, setOptions] = useState<string[]>(['', ''])
  const [multi, setMulti] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const qRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    qRef.current?.focus()
  }, [])

  // Esc closes — same gesture across overlays.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  function setOption(i: number, value: string) {
    setOptions((prev) => {
      const next = [...prev]
      next[i] = value
      // Auto-grow: if the user filled the last row and we have room, add a
      // fresh blank one so they can keep typing without hunting for an
      // "Add option" button. Cap at 12 like WA.
      const lastIndex = next.length - 1
      if (i === lastIndex && value.trim() && next.length < 12) {
        next.push('')
      }
      return next
    })
  }

  function removeOption(i: number) {
    setOptions((prev) => (prev.length <= 2 ? prev : prev.filter((_, j) => j !== i)))
  }

  // Trim + dedupe before send so empty placeholder rows don't ship as
  // ghost options. The "valid" check below uses the same shape.
  function cleanedOptions(): string[] {
    const seen = new Set<string>()
    const out: string[] = []
    for (const o of options) {
      const t = o.trim()
      if (!t) continue
      if (seen.has(t)) continue
      seen.add(t)
      out.push(t)
    }
    return out
  }

  const canSend = question.trim().length > 0 && cleanedOptions().length >= 2 && !sending

  async function submit() {
    if (!canSend) return
    setSending(true)
    setError('')
    try {
      const opts = cleanedOptions()
      const max = multi ? opts.length : 1
      const res = await api.createPoll(jid, question.trim(), opts, max)
      onSent(res.message_id, question.trim(), opts, max)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to send poll')
    } finally {
      setSending(false)
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/70 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-[460px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Create poll</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-4 py-3">
          {error && <div className="mb-2 rounded bg-red-500/10 px-2 py-1 text-xs text-red-300">{error}</div>}

          <label className="mb-1 block text-[11px] uppercase tracking-wider text-neutral-500">
            Question
          </label>
          <input
            ref={qRef}
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="Ask something…"
            maxLength={255}
            dir="auto"
            className="mb-4 w-full rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-2 text-sm text-neutral-100 outline-none placeholder:text-neutral-600 focus:border-neutral-600"
          />

          <label className="mb-1 block text-[11px] uppercase tracking-wider text-neutral-500">
            Options
          </label>
          <div className="mb-3 flex flex-col gap-2">
            {options.map((opt, i) => (
              <div key={i} className="flex items-center gap-2">
                <input
                  value={opt}
                  onChange={(e) => setOption(i, e.target.value)}
                  placeholder={`Option ${i + 1}`}
                  maxLength={100}
                  dir="auto"
                  className="flex-1 rounded-lg border border-neutral-800 bg-neutral-950 px-3 py-1.5 text-sm text-neutral-100 outline-none placeholder:text-neutral-600 focus:border-neutral-600"
                />
                {options.length > 2 && (
                  <button
                    onClick={() => removeOption(i)}
                    title="Remove option"
                    aria-label={`Remove option ${i + 1}`}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-red-300"
                  >
                    ✕
                  </button>
                )}
              </div>
            ))}
          </div>

          <label className="flex cursor-pointer items-center gap-2 text-sm text-neutral-200">
            <input
              type="checkbox"
              checked={multi}
              onChange={(e) => setMulti(e.target.checked)}
              className="h-4 w-4 cursor-pointer accent-emerald-500"
            />
            Allow multiple answers
          </label>
        </div>

        <footer className="flex items-center justify-end gap-2 border-t border-neutral-800 px-4 py-3">
          <button
            onClick={onClose}
            disabled={sending}
            className="rounded-lg px-3 py-1.5 text-sm text-neutral-300 transition hover:bg-neutral-800 disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            onClick={submit}
            disabled={!canSend}
            className="rounded-lg bg-emerald-600 px-4 py-1.5 text-sm font-medium text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {sending ? 'Sending…' : 'Send poll'}
          </button>
        </footer>
      </div>
    </div>
  )
}

import { useEffect, useState } from 'react'
import { api, type HideChatPreview, type HideChatResult } from '../api'

// HideChatDialog confirms hiding one chat. It shows EXACTLY what AI-derived
// data will be deleted (tasks, profile, transcripts, etc.). Hiding a chat is
// irreversible for that derived data — the message history itself stays.
export function HideChatDialog({
  jid,
  title,
  onDone,
  onClose,
}: {
  jid: string
  title: string
  onDone: (res: HideChatResult) => void
  onClose: () => void
}) {
  const [preview, setPreview] = useState<HideChatPreview | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    api
      .hideChatPreview(jid)
      .then(setPreview)
      .catch((e) => setErr((e as Error).message))
  }, [jid])

  async function confirm() {
    setBusy(true)
    try {
      const res = await api.hideChat(jid)
      onDone(res)
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-950 p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-2 flex items-center justify-between">
          <div className="text-sm font-semibold">🔒 Hide this chat</div>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>
        <p dir="auto" className="mb-3 truncate text-xs text-neutral-500">
          {title}
        </p>

        {err && <div className="mb-3 text-xs text-red-400">{err}</div>}
        {!preview && !err && (
          <div className="py-6 text-center text-sm text-neutral-600">Checking…</div>
        )}

        {preview && (
          <div className="space-y-3">
            <p className="text-xs text-neutral-300">
              The chat will be hidden from every list and from every AI feature. The message
              history itself stays. The AI-derived data below will be{' '}
              <span className="text-red-300">permanently deleted</span> right now:
            </p>

            <ul className="space-y-1 rounded-lg border border-neutral-800 bg-neutral-900/40 p-3 text-xs text-neutral-300">
              <Item n={preview.tasks_originated_here} label="task(s) originated here" />
              <Item n={preview.tasks_linked} label="task(s) with links to this chat" />
              <Item n={preview.task_message_links} label="message link(s) on cross-chat tasks" />
              {preview.profile_exists && <li>• AI purpose description</li>}
              <Item n={preview.media_understanding_rows} label="voice/image analysis row(s)" />
              {preview.extraction_watermark_set && <li>• Extraction history pointer</li>}
              <Item n={preview.circle_membership_count} label="circle membership(s)" />
              <li>• Today’s briefing (regenerates on next click)</li>
              <li>• AI extraction session files for this chat</li>
            </ul>

            <p className="text-[11px] text-neutral-500">
              To see this chat again, enter your PIN in the search bar and approve with Touch ID.
              You can unhide it from the locked-chats list.
            </p>

            <div className="flex gap-2">
              <button
                onClick={confirm}
                disabled={busy}
                className="flex-1 rounded-lg bg-red-500 px-3 py-2 text-sm font-semibold text-neutral-50 hover:bg-red-400 disabled:opacity-50"
              >
                {busy ? 'Hiding…' : 'Hide & delete AI data'}
              </button>
              <button
                onClick={onClose}
                className="rounded-lg border border-neutral-700 px-3 py-2 text-sm text-neutral-300 hover:bg-neutral-800"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function Item({ n, label }: { n: number; label: string }) {
  if (n <= 0) return null
  return (
    <li>
      • <span className="font-mono text-neutral-100">{n}</span> {label}
    </li>
  )
}

import { useEffect, useState } from 'react'
import { api } from '../api'

type Draft = { text: string; style?: string; reason?: string }

// DraftRepliesPopover asks the bridge for 2-3 candidate replies to this chat
// and lets the user pick one. The picked text is returned via onPick, which
// the parent uses to fill the composer.
export function DraftRepliesPopover({
  jid,
  onPick,
  onClose,
}: {
  jid: string
  onPick: (text: string) => void
  onClose: () => void
}) {
  const [drafts, setDrafts] = useState<Draft[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .draftReplies(jid)
      .then((r) => setDrafts(r.drafts || []))
      .catch((e) => setError((e as Error).message))
  }, [jid])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl rounded-2xl border border-neutral-800 bg-neutral-950 p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <div className="text-sm font-semibold">✨ Draft replies</div>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>

        {drafts === null && !error && (
          <div className="py-10 text-center text-sm text-neutral-500">
            Reading the chat and matching your tone…
          </div>
        )}
        {error && <div className="text-xs text-red-400">Failed: {error}</div>}
        {drafts && drafts.length === 0 && (
          <div className="text-sm text-neutral-500">
            Nothing actionable to reply to — the chat is either quiet or your last message was the
            most recent.
          </div>
        )}
        {drafts && drafts.length > 0 && (
          <div className="flex flex-col gap-2">
            {drafts.map((d, i) => (
              <button
                key={i}
                onClick={() => onPick(d.text)}
                className="rounded-lg border border-neutral-800 bg-neutral-900/40 px-3 py-2 text-left hover:border-neutral-600 hover:bg-neutral-900"
              >
                <div dir="auto" className="whitespace-pre-wrap text-sm text-neutral-100">
                  {d.text}
                </div>
                {(d.style || d.reason) && (
                  <div className="mt-1 text-[11px] text-neutral-500">
                    {d.style && (
                      <span className="mr-2 rounded bg-neutral-800 px-1.5 py-0.5">{d.style}</span>
                    )}
                    {d.reason}
                  </div>
                )}
              </button>
            ))}
          </div>
        )}
        <p className="mt-3 text-[11px] text-neutral-600">
          Picking a draft fills the composer — you can still edit before sending.
        </p>
      </div>
    </div>
  )
}

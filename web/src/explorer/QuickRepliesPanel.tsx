import { useEffect, useState } from 'react'
import { useQuickReplies } from '../hooks/useQuickReplies'

// QuickRepliesPanel — the manager modal for WhatsApp-Business-style quick
// replies (canned responses). Opened from the sidebar ⋮ More menu or from
// the composer picker's "Manage" link. Add / edit / delete saved replies;
// everything persists to localStorage via useQuickReplies.
export function QuickRepliesPanel({ onClose }: { onClose: () => void }) {
  const { replies, add, update, remove } = useQuickReplies()
  const [title, setTitle] = useState('')
  const [text, setText] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)

  // Esc closes — same dismissal contract as the other modals.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const resetForm = () => {
    setTitle('')
    setText('')
    setEditingId(null)
  }

  const submit = () => {
    if (!text.trim()) return
    if (editingId) update(editingId, title, text)
    else add(title, text)
    resetForm()
  }

  const startEdit = (id: string) => {
    const r = replies.find((x) => x.id === id)
    if (!r) return
    setEditingId(id)
    setTitle(r.title)
    setText(r.text)
  }

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex h-[80vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl bg-neutral-900 shadow-xl ring-1 ring-neutral-800"
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-neutral-100">
            <svg
              viewBox="0 0 24 24"
              width="15"
              height="15"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="text-emerald-300"
              aria-hidden="true"
            >
              <path d="M3 11.5a8.38 8.38 0 0 1 8.5-8.5 8.5 8.5 0 0 1 8.5 8.5 8.38 8.38 0 0 1-8.5 8.5 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8z" />
              <path d="m8 11 2.5 2.5L15 9" />
            </svg>
            Quick replies
          </h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* List of saved replies */}
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          {replies.length === 0 ? (
            <p className="py-8 text-center text-sm text-neutral-500">
              No quick replies yet. Save a canned response below to reuse it in any chat.
            </p>
          ) : (
            <ul className="space-y-2">
              {replies.map((r) => (
                <li
                  key={r.id}
                  className="group flex items-start justify-between gap-3 rounded-xl border border-neutral-800 bg-neutral-950/40 px-3 py-2"
                >
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium text-neutral-200">{r.title}</div>
                    <div className="whitespace-pre-wrap break-words text-xs text-neutral-500">{r.text}</div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <button
                      onClick={() => startEdit(r.id)}
                      title="Edit"
                      aria-label="Edit quick reply"
                      className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-100"
                    >
                      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <path d="M12 20h9" />
                        <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4z" />
                      </svg>
                    </button>
                    <button
                      onClick={() => {
                        if (editingId === r.id) resetForm()
                        remove(r.id)
                      }}
                      title="Delete"
                      aria-label="Delete quick reply"
                      className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-red-500/15 hover:text-red-300"
                    >
                      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M6 6l1 14a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-14" />
                      </svg>
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* Add / edit form */}
        <div className="space-y-2 border-t border-neutral-800 px-4 py-3">
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Label (e.g. Greeting)"
            className="w-full rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') submit()
            }}
            placeholder="Message text…"
            rows={2}
            className="w-full resize-none rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-emerald-500/60 focus:outline-none"
          />
          <div className="flex items-center justify-end gap-2">
            {editingId && (
              <button
                onClick={resetForm}
                className="rounded-lg px-3 py-1.5 text-sm text-neutral-400 transition hover:text-neutral-200"
              >
                Cancel
              </button>
            )}
            <button
              onClick={submit}
              disabled={!text.trim()}
              className="rounded-lg bg-emerald-600 px-3 py-1.5 text-sm font-medium text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {editingId ? 'Save changes' : 'Add quick reply'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

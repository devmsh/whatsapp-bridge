import { useQuickReplies } from '../hooks/useQuickReplies'

// QuickReplyPicker — the small popover the composer pops above its
// quick-reply button. Pick a saved reply to drop its text into the
// composer; "Manage" fires a window event the Explorer listens for to
// open the full manager panel (avoids prop-drilling through MessageThread).
export function QuickReplyPicker({
  onPick,
  onClose,
}: {
  onPick: (text: string) => void
  onClose: () => void
}) {
  const { replies } = useQuickReplies()

  const openManager = () => {
    window.dispatchEvent(new CustomEvent('wa.open-quick-replies'))
    onClose()
  }

  return (
    <>
      {/* Click-away backdrop — same dismissal contract as the sticker tray. */}
      <div className="fixed inset-0 z-20" onClick={onClose} />
      <div className="absolute bottom-12 left-0 z-30 w-72 overflow-hidden rounded-xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60">
        <div className="flex items-center justify-between border-b border-neutral-800 px-3 py-2">
          <span className="text-xs font-semibold text-neutral-300">Quick replies</span>
          <button
            onClick={openManager}
            className="text-[11px] text-emerald-300 transition hover:text-emerald-200"
          >
            Manage
          </button>
        </div>
        {replies.length === 0 ? (
          <div className="px-3 py-4 text-center text-xs text-neutral-500">
            No quick replies yet.{' '}
            <button
              onClick={openManager}
              className="text-emerald-300 transition hover:text-emerald-200"
            >
              Add one
            </button>
          </div>
        ) : (
          <ul className="max-h-64 overflow-y-auto py-1">
            {replies.map((r) => (
              <li key={r.id}>
                <button
                  onClick={() => {
                    onPick(r.text)
                    onClose()
                  }}
                  className="block w-full px-3 py-2 text-left transition hover:bg-neutral-800"
                  title={r.text}
                >
                  <div className="truncate text-sm font-medium text-neutral-200">{r.title}</div>
                  <div className="truncate text-xs text-neutral-500">{r.text}</div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  )
}

import type { Chat, Circle, Contact, Group, Tag } from '../api'
import { CircleView } from './CircleView'

// AdminSlideOver wraps CircleView in a dismissible slide-over panel that
// overlays the normal dashboard from the right, instead of replacing it
// outright (the old "Manage" full-screen takeover behavior). The dashboard
// underneath stays mounted; this is purely an overlay shell.
export function AdminSlideOver({
  open,
  onClose,
  circleId,
  circles,
  chats,
  contacts,
  groups,
  nameMap,
  allTags,
  onTagsChanged,
  onOpenChat,
  onOpenCircle,
  onOpenTasks,
  onChanged,
  onDeleted,
}: {
  open: boolean
  onClose: () => void
  circleId: number
  circles: Circle[]
  chats: Chat[]
  contacts: Contact[]
  groups: Group[]
  nameMap: Map<string, string>
  allTags: Tag[]
  onTagsChanged: () => void
  onOpenChat: (jid: string) => void
  onOpenCircle: (id: number) => void
  onOpenTasks: (id: number) => void
  onChanged: () => void
  onDeleted: () => void
}) {
  if (!open) return null

  return (
    <div className="fixed inset-0 z-40 bg-black/60" onClick={onClose}>
      <div
        className={
          'fixed right-0 top-0 z-50 flex h-full w-full max-w-2xl flex-col border-l border-neutral-800 bg-neutral-950 shadow-2xl transition-transform duration-200 ' +
          (open ? 'translate-x-0' : 'translate-x-full')
        }
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center justify-between border-b border-neutral-800 px-4 py-3">
          <span className="text-sm font-semibold text-neutral-100">Manage</span>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2 py-1 text-sm text-neutral-300 hover:bg-neutral-800"
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-hidden">
          <CircleView
            circleId={circleId}
            circles={circles}
            chats={chats}
            contacts={contacts}
            groups={groups}
            nameMap={nameMap}
            allTags={allTags}
            onTagsChanged={onTagsChanged}
            onOpenChat={onOpenChat}
            onOpenCircle={onOpenCircle}
            onOpenTasks={onOpenTasks}
            onChanged={onChanged}
            onDeleted={() => {
              onClose()
              onDeleted()
            }}
          />
        </div>
      </div>
    </div>
  )
}

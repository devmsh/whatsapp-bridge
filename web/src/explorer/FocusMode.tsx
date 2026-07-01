import type { Chat, Circle } from '../api'

// FocusMode is a full-screen, per-circle takeover: when active, Explorer
// renders ONLY this component (tab bar, aside, and main are not rendered).
// This iteration (iter 1) ships the shell plus two placeholder slots; T05
// wires in the real <FocusProfile> and <FocusChatList> panels. The grid
// layout below is deliberately built to accept more panels later (task
// board, digest) without a rewrite.
export function FocusMode({
  circleId,
  circles,
  chats,
  nameMap,
  onOpenChat,
  onExit,
}: {
  circleId: number
  circles: Circle[]
  chats: Chat[]
  nameMap: Map<string, string>
  onOpenChat: (jid: string) => void
  onExit: () => void
}) {
  const circle = circles.find((c) => c.id === circleId)

  // `chats`, `nameMap`, and `onOpenChat` are unused until T05 wires the real
  // <FocusProfile>/<FocusChatList> panels into the slots below; referencing
  // them here (without changing their names) keeps `noUnusedParameters` happy
  // in the interim without pre-wiring components that may not exist yet.
  void chats
  void nameMap
  void onOpenChat

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-neutral-950 text-neutral-100">
      <header className="flex shrink-0 items-center gap-3 border-b border-neutral-800 px-4 py-3">
        <span
          className="h-3.5 w-3.5 shrink-0 rounded-full"
          style={{ backgroundColor: circle?.color || '#737373' }}
        />
        <h1 className="min-w-0 flex-1 truncate text-lg font-semibold">
          {circle?.name || `Circle ${circleId}`}
        </h1>
        <button
          onClick={onExit}
          className="shrink-0 rounded-lg border border-neutral-700 px-3 py-1.5 text-sm text-neutral-300 hover:bg-neutral-800"
        >
          Exit Focus
        </button>
      </header>

      {/*
        Content grid: named areas so future panels (task board, digest) can
        be added as new grid-template-areas rows/columns without reworking
        the panels already here. For now it's a simple two-column layout:
        profile on the left, chat list on the right.
      */}
      <div
        className="grid min-h-0 flex-1 gap-4 overflow-hidden p-4"
        style={{ gridTemplateColumns: 'minmax(280px, 1fr) minmax(320px, 1.4fr)' }}
      >
        <div className="min-h-0 overflow-y-auto rounded-lg border border-neutral-800">
          {/* profile slot: T05 replaces this with <FocusProfile circleId={circleId} circles={circles} nameMap={nameMap} /> */}
          <div className="p-4 text-sm text-neutral-500">Profile panel coming soon.</div>
        </div>

        <div className="min-h-0 overflow-y-auto rounded-lg border border-neutral-800">
          {/* chat-list slot: T05 replaces this with <FocusChatList circleId={circleId} chats={chats} nameMap={nameMap} onOpenChat={onOpenChat} /> */}
          <div className="p-4 text-sm text-neutral-500">Chat list panel coming soon.</div>
        </div>
      </div>
    </div>
  )
}

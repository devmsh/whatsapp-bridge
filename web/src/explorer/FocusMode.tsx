import type { Chat, Circle } from '../api'
import { FocusChatList } from './FocusChatList'
import { FocusProfile } from './FocusProfile'
import { FocusSwitcher } from './FocusSwitcher'

// FocusMode is a full-screen, per-circle takeover: when active, Explorer
// renders ONLY this component (tab bar, aside, and main are not rendered).
// The grid layout below is deliberately built to accept more panels later
// (task board, digest) without a rewrite.
export function FocusMode({
  circleId,
  circles,
  chats,
  nameMap,
  onOpenChat,
  onExit,
  onSwitchCircle,
}: {
  circleId: number
  circles: Circle[]
  chats: Chat[]
  nameMap: Map<string, string>
  onOpenChat: (jid: string) => void
  onExit: () => void
  onSwitchCircle: (id: number) => void
}) {
  const circle = circles.find((c) => c.id === circleId)

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
        <FocusSwitcher circles={circles} activeCircleId={circleId} onSelect={onSwitchCircle} />
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
          {/* profile slot */}
          <FocusProfile circleId={circleId} circles={circles} nameMap={nameMap} />
        </div>

        <div className="min-h-0 overflow-y-auto rounded-lg border border-neutral-800">
          {/* chat-list slot */}
          <FocusChatList circleId={circleId} chats={chats} nameMap={nameMap} onOpenChat={onOpenChat} />
        </div>
      </div>
    </div>
  )
}

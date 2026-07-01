import { useEffect, useState } from 'react'
import type { Chat, Circle, Message, Tag, Task } from '../api'
import type { MentionEntry } from './format'
import { FocusChatList } from './FocusChatList'
import { FocusDigest } from './FocusDigest'
import { FocusProfile } from './FocusProfile'
import { FocusSwitcher } from './FocusSwitcher'
import { FocusTasks } from './FocusTasks'
import { MessageThread } from './MessageThread'

// FocusMode is a full-screen, per-circle takeover: when active, Explorer
// renders ONLY this component (tab bar, aside, and main are not rendered).
// The grid layout below is deliberately built to accept more panels later
// (digest) without a rewrite.
export function FocusMode({
  circleId,
  circles,
  chats,
  nameMap,
  mentionIndex,
  selfDigits,
  liveMsg,
  allTags,
  contactTags,
  chatDrafts,
  consumeChatDraft,
  allTasks,
  ownJID,
  onOpenChat,
  onExit,
  onSwitchCircle,
  onCirclesChanged,
  onTagsChanged,
  onOpenTask,
  onTasksChanged,
  onOpenChatTasks,
  onOpenCircle,
  onSent,
  pendingJumpId,
  onJumpHandled,
}: {
  circleId: number
  circles: Circle[]
  chats: Chat[]
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  selfDigits?: Set<string>
  liveMsg: Message | null
  allTags: Tag[]
  contactTags: Record<string, Tag[]>
  chatDrafts: Record<string, string>
  consumeChatDraft: (jid: string) => void
  allTasks: Task[]
  ownJID: string
  onOpenChat: (jid: string, draft?: string) => void
  onExit: () => void
  onSwitchCircle: (id: number) => void
  onCirclesChanged: () => void
  onTagsChanged: () => void
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChatTasks: (jid: string) => void
  onOpenCircle: (id: number) => void
  onSent?: (m: Message) => void
  pendingJumpId?: string | null
  onJumpHandled?: () => void
}) {
  const circle = circles.find((c) => c.id === circleId)
  const [activeChatJid, setActiveChatJid] = useState<string | null>(null)

  // Switching the focused circle (via the switcher) should not leave a
  // stale thread open for a chat that may not belong to the new circle.
  useEffect(() => setActiveChatJid(null), [circleId])

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
        {((circle?.parent_ids?.length ?? 0) > 0 || (circle?.child_circles?.length ?? 0) > 0) && (
          <div className="flex min-w-0 shrink-0 flex-wrap items-center gap-1.5">
            {circle?.parent_ids?.map((parentId) => {
              const parent = circles.find((c) => c.id === parentId)
              if (!parent) return null
              return (
                <button
                  key={`parent-${parentId}`}
                  onClick={() => onSwitchCircle(parentId)}
                  className="shrink-0 rounded-full border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
                  title={`Go to parent circle: ${parent.name}`}
                >
                  ↑ {parent.name}
                </button>
              )
            })}
            {circle?.child_circles?.map((childId) => {
              const child = circles.find((c) => c.id === childId)
              if (!child) return null
              return (
                <button
                  key={`child-${childId}`}
                  onClick={() => onSwitchCircle(childId)}
                  className="shrink-0 rounded-full border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
                  title={`Go to sub-circle: ${child.name}`}
                >
                  ↳ {child.name}
                </button>
              )
            })}
          </div>
        )}
        <FocusSwitcher circles={circles} activeCircleId={circleId} onSelect={onSwitchCircle} />
        <button
          onClick={onExit}
          className="shrink-0 rounded-lg border border-neutral-700 px-3 py-1.5 text-sm text-neutral-300 hover:bg-neutral-800"
        >
          Exit Focus
        </button>
      </header>

      {/*
        Content grid: named areas so future panels (digest) can be added as
        new grid-template-areas rows/columns without reworking the panels
        already here. For now it's a two-column layout: profile + task board
        on the left, chat list / inline thread on the right.
      */}
      <div
        className="grid min-h-0 flex-1 gap-4 overflow-hidden p-4"
        style={{ gridTemplateColumns: 'minmax(280px, 1fr) minmax(320px, 1.4fr)' }}
      >
        <div className="flex min-h-0 flex-col gap-4">
          <div className="min-h-0 flex-1 overflow-hidden rounded-lg border border-neutral-800">
            {/* digest slot */}
            <FocusDigest circleId={circleId} onOpenTask={onOpenTask} onOpenChat={onOpenChat} />
          </div>
          <div className="min-h-0 flex-1 overflow-hidden rounded-lg border border-neutral-800">
            {/* profile slot */}
            <FocusProfile circleId={circleId} circles={circles} nameMap={nameMap} />
          </div>
          <div className="min-h-0 flex-1 overflow-hidden rounded-lg border border-neutral-800">
            {/* task-board slot */}
            <FocusTasks
              circleId={circleId}
              tasks={allTasks}
              circles={circles}
              chats={chats}
              nameMap={nameMap}
              ownJID={ownJID}
              onOpenTask={onOpenTask}
              onCreated={onTasksChanged}
              onChanged={onTasksChanged}
            />
          </div>
        </div>

        {activeChatJid == null ? (
          <div className="min-h-0 overflow-y-auto rounded-lg border border-neutral-800">
            {/* chat-list slot */}
            <FocusChatList
              circleId={circleId}
              chats={chats}
              nameMap={nameMap}
              onSelectChat={setActiveChatJid}
            />
          </div>
        ) : (
          <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-neutral-800">
            <button
              onClick={() => setActiveChatJid(null)}
              className="shrink-0 self-start px-3 py-2 text-xs text-neutral-400 hover:text-neutral-200"
            >
              ← Back to chats
            </button>
            <div className="min-h-0 flex-1">
              <MessageThread
                jid={activeChatJid}
                chats={chats}
                nameMap={nameMap}
                mentionIndex={mentionIndex}
                selfDigits={selfDigits}
                liveMsg={liveMsg}
                circles={circles}
                allTags={allTags}
                contactTags={contactTags}
                initialDraft={chatDrafts[activeChatJid] || ''}
                onDraftConsumed={() => consumeChatDraft(activeChatJid)}
                onCirclesChanged={onCirclesChanged}
                onTagsChanged={onTagsChanged}
                onOpenTask={onOpenTask}
                onTasksChanged={onTasksChanged}
                onOpenChatTasks={onOpenChatTasks}
                onOpenChat={onOpenChat}
                onOpenCircle={onOpenCircle}
                onSent={onSent}
                pendingJumpId={pendingJumpId}
                onJumpHandled={onJumpHandled}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

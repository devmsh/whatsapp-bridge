import { useEffect, useState } from 'react'
import type { Chat, Circle, Contact, Group, Message, Tag, Task } from '../api'
import type { MentionEntry } from './format'
import { AdminSlideOver } from './AdminSlideOver'
import { CirclePopover } from './CirclePopover'
import { FocusStream } from './FocusStream'
import { FocusSwitcher } from './FocusSwitcher'
import { FocusTasks } from './FocusTasks'
import { MessageThread } from './MessageThread'

// FocusMode is a full-screen, per-circle takeover: when active, Explorer
// renders ONLY this component (tab bar, aside, and main are not rendered).
// The daily-driver surface is FocusStream: one ranked, actionable list
// (needs-you / moving / quiet), not separate digest/profile/task-board
// panels. Circle purpose+members live in CirclePopover (anchored to the
// circle name); rare admin actions live in AdminSlideOver — both overlays,
// never competing with the daily view for primary screen space.
export function FocusMode({
  circleId,
  circles,
  chats,
  contacts,
  groups,
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
  initialManaging,
  onOpenChat,
  onExit,
  onSwitchCircle,
  onCirclesChanged,
  onTagsChanged,
  onOpenTask,
  onTasksChanged,
  onOpenChatTasks,
  onOpenTasks,
  onOpenCircle,
  onSent,
  pendingJumpId,
  onJumpHandled,
}: {
  circleId: number
  circles: Circle[]
  chats: Chat[]
  contacts: Contact[]
  groups: Group[]
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
  initialManaging: boolean
  onOpenChat: (jid: string, draft?: string) => void
  onExit: () => void
  onSwitchCircle: (id: number) => void
  onCirclesChanged: () => void
  onTagsChanged: () => void
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChatTasks: (jid: string) => void
  onOpenTasks: (id: number) => void
  onOpenCircle: (id: number) => void
  onSent?: (m: Message) => void
  pendingJumpId?: string | null
  onJumpHandled?: () => void
}) {
  const circle = circles.find((c) => c.id === circleId)
  const [activeChatJid, setActiveChatJid] = useState<string | null>(null)
  // Whether Focus Mode is showing the "⚙ Manage" screen (circle rename,
  // members, keyword suggestions, sub-circles, task extraction, export) in
  // place of the normal dashboard. Seeded from initialManaging — a plain
  // useState initializer, NOT a [circleId]-keyed effect: FocusMode only
  // mounts fresh on a genuine null→non-null entry (Explorer's early-return
  // stops rendering it on exit), so the initializer captures "was Manage
  // intent set at entry". Switching circles mid-session (via the switcher or
  // the breadcrumb) changes circleId WITHOUT unmounting FocusMode, which
  // correctly PRESERVES whichever mode the user was already in.
  const [managing, setManaging] = useState(initialManaging)
  // Whether the circle-purpose/members popover (anchored to the circle name
  // in the header) is open. Replaces the old permanent FocusProfile panel.
  const [showProfile, setShowProfile] = useState(false)
  // Whether the right pane is showing the full task board (triggered by
  // FocusStream's "See all tasks" link, for tasks the ranked stream can't
  // anchor to a chat) instead of its default idle state.
  const [browsingAllTasks, setBrowsingAllTasks] = useState(false)

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
        <div className="relative min-w-0 flex-1">
          <button
            onClick={() => setShowProfile((v) => !v)}
            aria-pressed={showProfile}
            className="max-w-full truncate rounded px-1 text-left text-lg font-semibold hover:bg-neutral-800"
            title="Show circle purpose & members"
          >
            {circle?.name || `Circle ${circleId}`}
          </button>
          <CirclePopover
            open={showProfile}
            onClose={() => setShowProfile(false)}
            circleId={circleId}
            circles={circles}
            nameMap={nameMap}
          />
        </div>
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
          onClick={() => setManaging((v) => !v)}
          aria-pressed={managing}
          className={
            'shrink-0 rounded-lg border px-3 py-1.5 text-sm transition ' +
            (managing
              ? 'border-emerald-600 bg-emerald-500/15 text-emerald-300'
              : 'border-neutral-700 text-neutral-300 hover:bg-neutral-800')
          }
        >
          ⚙ Manage
        </button>
        <button
          onClick={onExit}
          className="shrink-0 rounded-lg border border-neutral-700 px-3 py-1.5 text-sm text-neutral-300 hover:bg-neutral-800"
        >
          Exit Focus
        </button>
      </header>

      {/*
        Two-column layout: FocusStream (the entire daily-driver surface — one
        ranked, actionable list) on the left; a reading pane on the right
        that shows an open chat thread, the full task board (only when
        explicitly requested via "See all tasks"), or an idle placeholder.
        "Manage" and circle purpose/members don't live in this grid at all —
        they overlay it via AdminSlideOver / CirclePopover above, so this
        dashboard stays mounted underneath.
      */}
      <div
        className="grid min-h-0 flex-1 gap-4 overflow-hidden p-4"
        style={{ gridTemplateColumns: 'minmax(280px, 1fr) minmax(320px, 1.4fr)' }}
      >
        <div className="min-h-0 overflow-hidden rounded-lg border border-neutral-800">
          <FocusStream
            circleId={circleId}
            chats={chats}
            allTasks={allTasks}
            nameMap={nameMap}
            onSelectChat={setActiveChatJid}
            onOpenTask={onOpenTask}
            onTasksChanged={onTasksChanged}
            onBrowseAllTasks={() => setBrowsingAllTasks(true)}
          />
        </div>

        {activeChatJid == null ? (
          <div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-neutral-800">
            {browsingAllTasks ? (
              <>
                <button
                  onClick={() => setBrowsingAllTasks(false)}
                  className="shrink-0 self-start px-3 py-2 text-xs text-neutral-400 hover:text-neutral-200"
                >
                  ← Back
                </button>
                <div className="min-h-0 flex-1">
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
              </>
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-neutral-600">
                Select a chat from the stream, or browse all tasks.
              </div>
            )}
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

      <AdminSlideOver
        open={managing}
        onClose={() => setManaging(false)}
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
        onChanged={onCirclesChanged}
        onDeleted={() => {
          setManaging(false)
          onExit()
        }}
      />
    </div>
  )
}

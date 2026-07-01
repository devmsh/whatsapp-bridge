import { useEffect, useState } from 'react'
import { api, type Chat } from '../api'
import { isGroup } from './format'
import { ChatAvatar } from './ChatAvatar'

// FocusChatList shows this circle's flattened chats only (a filtered subset
// of the global `chats` list), each rendered with its own minimal row. It
// deliberately does NOT reuse ChatList.tsx's row — that component closes
// over too much local state (drafts, typing, context menus) to reuse safely
// here — so this is a read-only, simplified view.
export function FocusChatList({
  circleId,
  chats,
  nameMap,
  onSelectChat,
}: {
  circleId: number
  chats: Chat[]
  nameMap: Map<string, string>
  onSelectChat: (jid: string) => void
}) {
  const [jids, setJids] = useState<Set<string> | null>(null)

  useEffect(() => {
    let cancelled = false
    setJids(null)
    api.circleChats(circleId).then((list) => {
      if (!cancelled) setJids(new Set(list))
    })
    return () => {
      cancelled = true
    }
  }, [circleId])

  if (jids === null) {
    return <div className="p-4 text-xs text-neutral-500">Loading chats…</div>
  }

  const rows = chats.filter((c) => jids.has(c.jid))

  if (rows.length === 0) {
    return <div className="p-4 text-xs text-neutral-600">No chats in this circle</div>
  }

  return (
    <div className="flex flex-col overflow-y-auto">
      {rows.map((chat) => {
        const title = nameMap.get(chat.jid) || chat.name
        return (
          <button
            key={chat.jid}
            onClick={() => onSelectChat(chat.jid)}
            className="flex w-full items-center gap-3 border-b border-neutral-900 px-3 py-2.5 text-left transition hover:bg-neutral-900"
          >
            <ChatAvatar jid={chat.jid} title={title} group={isGroup(chat.jid)} size={36} />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span dir="auto" className="truncate text-sm font-medium">
                  {title}
                </span>
                {chat.is_muted && (
                  <span title="Muted" aria-label="Muted" className="shrink-0 text-neutral-500">
                    <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
                      <path d="M18.63 13A17.89 17.89 0 0 1 18 8" />
                      <path d="M6.26 6.26A5.86 5.86 0 0 0 6 8c0 7-3 9-3 9h14" />
                      <path d="M18 8a6 6 0 0 0-9.33-5" />
                      <line x1="1" y1="1" x2="23" y2="23" />
                    </svg>
                  </span>
                )}
              </div>
            </div>
            <div className="ml-1 flex shrink-0 items-center gap-1">
              {(chat.unread_mentions ?? 0) > 0 && (
                <span
                  title={chat.unread_mentions === 1 ? 'You were mentioned' : `${chat.unread_mentions} mentions of you`}
                  className="shrink-0 rounded-full bg-emerald-500 px-1 text-[11px] font-semibold leading-tight text-neutral-950"
                >
                  @
                </span>
              )}
              {chat.unread_count > 0 && (
                <span
                  className={
                    'shrink-0 rounded-full px-1.5 py-0.5 text-[11px] font-semibold ' +
                    (chat.is_muted ? 'bg-neutral-700 text-neutral-300' : 'bg-emerald-500 text-neutral-950')
                  }
                >
                  {chat.unread_count}
                </span>
              )}
            </div>
          </button>
        )
      })}
    </div>
  )
}

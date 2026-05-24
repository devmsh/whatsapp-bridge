import { useMemo, useState } from 'react'
import type { Chat } from '../api'
import { chatListTime, chatTitle, initial, isGroup, previewText } from './format'

// ChatList shows the searchable, time-ordered list of chats.
export function ChatList({
  chats,
  nameMap,
  selected,
  onOpen,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  selected: string | null
  onOpen: (jid: string) => void
}) {
  const [q, setQ] = useState('')

  const rows = useMemo(() => {
    const withTitle = chats.map((c) => ({ chat: c, title: chatTitle(c, nameMap) }))
    const needle = q.trim().toLowerCase()
    const filtered = needle
      ? withTitle.filter(
          (r) => r.title.toLowerCase().includes(needle) || r.chat.jid.toLowerCase().includes(needle),
        )
      : withTitle
    return filtered
  }, [chats, nameMap, q])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="p-2">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search chats"
          className="w-full rounded-lg border border-neutral-800 bg-neutral-900 px-3 py-2 text-sm outline-none placeholder:text-neutral-600 focus:border-neutral-600"
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {rows.length === 0 && <div className="p-4 text-center text-xs text-neutral-600">No chats</div>}
        {rows.map(({ chat, title }) => (
          <button
            key={chat.jid}
            onClick={() => onOpen(chat.jid)}
            className={
              'flex w-full items-center gap-3 px-3 py-2.5 text-left transition ' +
              (selected === chat.jid ? 'bg-neutral-800' : 'hover:bg-neutral-900')
            }
          >
            <Avatar title={title} group={isGroup(chat.jid)} />
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <span dir="auto" className="truncate text-sm font-medium">
                  {title}
                </span>
                <span className="shrink-0 text-[11px] text-neutral-500">
                  {chatListTime(chat.last_message_at)}
                </span>
              </div>
              <div dir="auto" className="truncate text-xs text-neutral-500">
                {chat.last_message
                  ? previewText(chat.last_message, nameMap)
                  : isGroup(chat.jid)
                    ? 'Group'
                    : chat.jid.replace('@s.whatsapp.net', '')}
              </div>
            </div>
            {chat.unread_count > 0 && (
              <span className="ml-1 shrink-0 rounded-full bg-emerald-500 px-1.5 py-0.5 text-[11px] font-semibold text-neutral-950">
                {chat.unread_count}
              </span>
            )}
          </button>
        ))}
      </div>
    </div>
  )
}

function Avatar({ title, group }: { title: string; group: boolean }) {
  return (
    <div
      className={
        'flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-sm font-semibold ' +
        (group ? 'bg-sky-600/30 text-sky-300' : 'bg-neutral-700 text-neutral-200')
      }
    >
      {initial(title)}
    </div>
  )
}

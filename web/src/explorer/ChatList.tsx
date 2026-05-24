import { useMemo, useState } from 'react'
import { api, type Chat } from '../api'
import { chatListTime, chatTitle, isGroup, previewText } from './format'
import { ChatAvatar } from './ChatAvatar'
import { ContextMenu, type MenuItem } from './ContextMenu'

// ChatList shows the searchable, time-ordered list of chats.
// Right-click on any row opens a context menu with quick actions (Open, Hide,
// Unhide, Archive, Pin, Mark read/unread) so the user can act without first
// opening the chat.
export function ChatList({
  chats,
  nameMap,
  selected,
  onOpen,
  onRequestHide,
  onChanged,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  selected: string | null
  onOpen: (jid: string) => void
  onRequestHide: (jid: string, title: string) => void
  onChanged: () => void
}) {
  const [q, setQ] = useState('')
  const [menu, setMenu] = useState<{ jid: string; title: string; x: number; y: number } | null>(null)

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
            onContextMenu={(e) => {
              e.preventDefault()
              setMenu({ jid: chat.jid, title, x: e.clientX, y: e.clientY })
            }}
            className={
              'flex w-full items-center gap-3 px-3 py-2.5 text-left transition ' +
              (selected === chat.jid ? 'bg-neutral-800' : 'hover:bg-neutral-900')
            }
          >
            <ChatAvatar jid={chat.jid} title={title} group={isGroup(chat.jid)} size={40} />
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
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          items={buildChatMenu(
            chats.find((c) => c.jid === menu.jid),
            menu.title,
            { onOpen, onRequestHide, onChanged },
          )}
          onClose={() => setMenu(null)}
        />
      )}
    </div>
  )
}

// buildChatMenu returns the context menu items for one chat row. The menu is
// state-aware: a hidden chat (only seen in "private mode" view) gets Unhide
// instead of Hide; pinned/archived/muted/read toggle their inverse.
function buildChatMenu(
  chat: Chat | undefined,
  title: string,
  cb: {
    onOpen: (jid: string) => void
    onRequestHide: (jid: string, title: string) => void
    onChanged: () => void
  },
): MenuItem[] {
  if (!chat) return []
  const jid = chat.jid

  // Fire-and-refresh helper for chatAction calls.
  const act = async (action: string) => {
    try { await api.chatAction(jid, action) } catch {}
    cb.onChanged()
  }

  const items: MenuItem[] = [
    { label: 'Open chat', icon: '↗', onClick: () => cb.onOpen(jid) },
    { divider: true },
    {
      label: chat.unread_count > 0 ? 'Mark as read' : 'Mark as unread',
      icon: chat.unread_count > 0 ? '✓' : '●',
      onClick: () => act(chat.unread_count > 0 ? 'read' : 'unread'),
    },
    {
      label: chat.is_pinned ? 'Unpin' : 'Pin to top',
      icon: '📌',
      onClick: () => act(chat.is_pinned ? 'unpin' : 'pin'),
    },
    {
      label: chat.is_muted ? 'Unmute' : 'Mute',
      icon: '🔇',
      onClick: () => act(chat.is_muted ? 'unmute' : 'mute'),
    },
    {
      label: chat.is_archived ? 'Unarchive' : 'Archive',
      icon: '📦',
      onClick: () => act(chat.is_archived ? 'unarchive' : 'archive'),
    },
    { divider: true },
  ]

  if (chat.is_hidden) {
    items.push({
      label: 'Unhide chat',
      icon: '🔓',
      onClick: async () => {
        try { await api.unhideChat(jid) } catch {}
        cb.onChanged()
      },
    })
  } else {
    items.push({
      label: 'Hide chat…',
      icon: '🔒',
      danger: true,
      onClick: () => cb.onRequestHide(jid, title),
    })
  }

  return items
}


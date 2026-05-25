import { useEffect, useMemo, useState } from 'react'
import { api, type Chat, type Circle } from '../api'
import { chatListTime, chatTitle, isGroup, previewText } from './format'
import { ChatAvatar } from './ChatAvatar'
import { ContextMenu, type MenuItem } from './ContextMenu'

// ChatList shows the time-ordered list of chats. Search is handled by the
// global SearchBar in the top bar — there's no per-list filter anymore.
// Right-click on any row opens a context menu with quick actions (Open, Hide,
// Unhide, Archive, Pin, Mark read/unread, Add/Remove from circle) so the user
// can act without first opening the chat.
export function ChatList({
  chats,
  nameMap,
  circles,
  selected,
  onOpen,
  onRequestHide,
  onChanged,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  circles: Circle[]
  selected: string | null
  onOpen: (jid: string) => void
  onRequestHide: (jid: string, title: string) => void
  onChanged: () => void
}) {
  const [menu, setMenu] = useState<{ jid: string; title: string; x: number; y: number } | null>(null)
  // When set, shows the circle-membership picker for this chat.
  const [picking, setPicking] = useState<{ jid: string; title: string } | null>(null)

  const rows = useMemo(
    () => chats.map((c) => ({ chat: c, title: chatTitle(c, nameMap) })),
    [chats, nameMap],
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
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
            {
              onOpen,
              onRequestHide,
              onChanged,
              onRequestAddToCircle: (jid, title) => setPicking({ jid, title }),
            },
          )}
          onClose={() => setMenu(null)}
        />
      )}
      {picking && (
        <CirclePickerModal
          jid={picking.jid}
          chatTitle={picking.title}
          circles={circles}
          onClose={() => setPicking(null)}
          onAdded={() => {
            setPicking(null)
            onChanged()
          }}
        />
      )}
    </div>
  )
}

// CirclePickerModal shows a tree of circles + sub-circles with a checkbox
// per row indicating whether the chat is a member. Clicking toggles
// membership (add or remove). The chat (group or DM contact) goes through the
// regular circle-members API.
function CirclePickerModal({
  jid,
  chatTitle,
  circles,
  onClose,
  onAdded,
}: {
  jid: string
  chatTitle: string
  circles: Circle[]
  onClose: () => void
  onAdded: () => void
}) {
  const [busy, setBusy] = useState<Set<number>>(new Set())
  const [error, setError] = useState<string | null>(null)
  // Locally-tracked member set so checkboxes flip instantly without a refetch.
  const [memberIDs, setMemberIDs] = useState<Set<number> | null>(null)
  const memberType: 'group' | 'contact' = jid.endsWith('@g.us') ? 'group' : 'contact'

  // Fetch which circles this chat is already in.
  useEffect(() => {
    api
      .circlesForMember(memberType, jid)
      .then((cs) => setMemberIDs(new Set((cs || []).map((c) => c.id))))
      .catch(() => setMemberIDs(new Set()))
  }, [jid, memberType])

  // Build the tree from parent_ids. A circle with no parent (or whose only
  // parents are not in this circles set) is a root. Each circle appears under
  // its FIRST listed parent so the user sees a clean tree even if a circle
  // happens to be a member of multiple parents.
  const tree = useMemo(() => buildCircleTree(circles), [circles])

  async function toggle(c: Circle) {
    if (busy.has(c.id) || !memberIDs) return
    setBusy((b) => new Set(b).add(c.id))
    setError(null)
    const isMember = memberIDs.has(c.id)
    try {
      if (isMember) {
        await api.removeCircleMember(c.id, memberType, jid)
        setMemberIDs((s) => {
          const n = new Set(s)
          n.delete(c.id)
          return n
        })
      } else {
        await api.addCircleMember(c.id, memberType, jid)
        setMemberIDs((s) => new Set(s).add(c.id))
      }
      onAdded()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy((b) => {
        const n = new Set(b)
        n.delete(c.id)
        return n
      })
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-950 p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3">
          <div className="text-sm font-semibold">Circles</div>
          <div dir="auto" className="truncate text-xs text-neutral-500">
            {chatTitle}
          </div>
        </div>
        {circles.length === 0 ? (
          <p className="py-6 text-center text-sm text-neutral-500">
            You don’t have any circles yet. Create one from the Circles tab.
          </p>
        ) : (
          <div className="flex max-h-[60vh] flex-col overflow-y-auto">
            {tree.map((node) => (
              <CircleTreeRow
                key={node.circle.id}
                node={node}
                depth={0}
                memberIDs={memberIDs || new Set()}
                busy={busy}
                onToggle={toggle}
              />
            ))}
          </div>
        )}
        {error && <div className="mt-2 text-xs text-red-400">{error}</div>}
        <div className="mt-3 text-right">
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

type CircleTreeNode = { circle: Circle; children: CircleTreeNode[] }

function buildCircleTree(circles: Circle[]): CircleTreeNode[] {
  const byID = new Map<number, Circle>()
  circles.forEach((c) => byID.set(c.id, c))
  const childrenOf = new Map<number, CircleTreeNode[]>()
  const roots: CircleTreeNode[] = []
  // Two-pass for stable ordering: first create empty nodes, then attach.
  const nodes = new Map<number, CircleTreeNode>()
  circles.forEach((c) => nodes.set(c.id, { circle: c, children: [] }))
  for (const c of circles) {
    const node = nodes.get(c.id)!
    const parents = (c.parent_ids || []).filter((id) => byID.has(id))
    if (parents.length === 0) {
      roots.push(node)
    } else {
      // First parent only — keeps the display a clean tree.
      const parentID = parents[0]
      const arr = childrenOf.get(parentID) || []
      arr.push(node)
      childrenOf.set(parentID, arr)
    }
  }
  for (const [parentID, kids] of childrenOf) {
    const parent = nodes.get(parentID)
    if (parent) parent.children = kids
  }
  return roots
}

function CircleTreeRow({
  node,
  depth,
  memberIDs,
  busy,
  onToggle,
}: {
  node: CircleTreeNode
  depth: number
  memberIDs: Set<number>
  busy: Set<number>
  onToggle: (c: Circle) => void
}) {
  const c = node.circle
  const isMember = memberIDs.has(c.id)
  const isBusy = busy.has(c.id)
  return (
    <>
      <button
        onClick={() => onToggle(c)}
        disabled={isBusy}
        style={{ paddingLeft: 8 + depth * 16 }}
        className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition hover:bg-neutral-900 disabled:opacity-50"
      >
        <span
          className={
            'flex h-4 w-4 shrink-0 items-center justify-center rounded border transition ' +
            (isMember
              ? 'border-emerald-500 bg-emerald-500 text-neutral-950'
              : 'border-neutral-600')
          }
        >
          {isMember ? '✓' : ''}
        </span>
        <span
          className="h-3 w-3 shrink-0 rounded-full"
          style={{ backgroundColor: c.color || '#737373' }}
        />
        <span dir="auto" className="min-w-0 flex-1 truncate text-neutral-200">
          {c.name}
        </span>
        {isBusy && <span className="text-xs text-neutral-500">…</span>}
      </button>
      {node.children.map((child) => (
        <CircleTreeRow
          key={child.circle.id}
          node={child}
          depth={depth + 1}
          memberIDs={memberIDs}
          busy={busy}
          onToggle={onToggle}
        />
      ))}
    </>
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
    onRequestAddToCircle: (jid: string, title: string) => void
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
    {
      label: 'Circles…',
      icon: '⭕',
      onClick: () => cb.onRequestAddToCircle(jid, title),
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


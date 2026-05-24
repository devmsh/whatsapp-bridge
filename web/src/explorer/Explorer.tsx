import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  api,
  type Chat,
  type ChatPreview,
  type Circle,
  type Contact,
  type DeviceInfo,
  type Group,
  type Message,
  type Tag,
} from '../api'
import { buildNameMap } from './format'
import { ChatList } from './ChatList'
import { ContactsPanel } from './ContactsPanel'
import { CirclesPanel } from './CirclesPanel'
import { CircleView } from './CircleView'
import { RecommendationsView } from './RecommendationsView'
import { TasksPanel } from './TasksPanel'
import { TaskView } from './TaskView'
import { MessageThread } from './MessageThread'
import { MediaSettings } from '../Settings'
import { ProfilingStatusModal } from './ProfilingStatus'

type Tab = 'chats' | 'contacts' | 'circles' | 'tasks'

// Explorer is the main app after onboarding: a chat list / contacts sidebar and
// a message thread, with live updates over SSE.
export function Explorer({ device }: { device?: DeviceInfo }) {
  const [chats, setChats] = useState<Chat[]>([])
  const [contacts, setContacts] = useState<Contact[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [activity, setActivity] = useState<Map<string, number>>(new Map())
  const [circles, setCircles] = useState<Circle[]>([])
  const [tags, setTags] = useState<Tag[]>([])
  const [contactTags, setContactTags] = useState<Record<string, Tag[]>>({})
  const [tab, setTab] = useState<Tab>('chats')
  const [selected, setSelected] = useState<string | null>(null)
  const [selectedCircle, setSelectedCircle] = useState<number | null>(null)
  const [recoOpen, setRecoOpen] = useState(false)
  const [selectedTask, setSelectedTask] = useState<number | null>(null)
  const [taskChatFilter, setTaskChatFilter] = useState<string | null>(null)
  const [taskCircleFilter, setTaskCircleFilter] = useState<number | null>(null)
  const [taskVersion, setTaskVersion] = useState(0)
  const [liveMsg, setLiveMsg] = useState<Message | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [showProfiling, setShowProfiling] = useState(false)
  // pending composer drafts per chat — set by "Nudge" / "Reply" buttons in
  // TaskView, consumed once by MessageThread when it opens that chat.
  const [chatDrafts, setChatDrafts] = useState<Record<string, string>>({})

  const selectedRef = useRef<string | null>(null)
  selectedRef.current = selected

  const nameMap = useMemo(() => buildNameMap(contacts, groups), [contacts, groups])

  useEffect(() => {
    api.chats().then((c) => setChats(c || [])).catch(() => {})
    api.contacts().then((c) => setContacts(c || [])).catch(() => {})
    api.groups().then((g) => setGroups(g || [])).catch(() => {})
    api
      .chatStats()
      .then((stats) => {
        const m = new Map<string, number>()
        for (const st of stats || []) m.set(st.chat_jid, st.count)
        setActivity(m)
      })
      .catch(() => {})
    api.circles().then((c) => setCircles(c || [])).catch(() => {})
    api.tags().then((t) => setTags(t || [])).catch(() => {})
    api.contactTagsMap().then((m) => setContactTags(m || {})).catch(() => {})
  }, [])

  const reloadCircles = useCallback(() => {
    api.circles().then((c) => setCircles(c || [])).catch(() => {})
  }, [])

  const reloadTags = useCallback(() => {
    api.tags().then((t) => setTags(t || [])).catch(() => {})
    api.contactTagsMap().then((m) => setContactTags(m || {})).catch(() => {})
  }, [])

  // Live message stream: append to the open chat and reorder the chat list.
  useEffect(() => {
    let closed = false
    let es: EventSource | null = null
    function connect() {
      if (closed) return
      es = new EventSource('/api/v2/stream')
      es.onmessage = (e) => {
        let m: Message
        try {
          m = JSON.parse(e.data)
        } catch {
          return
        }
        if (!m || !m.chat_jid) return
        setLiveMsg(m)
        setChats((prev) => bumpChat(prev, m, selectedRef.current))
      }
      es.onerror = () => {
        es?.close()
        if (!closed) setTimeout(connect, 1500)
      }
    }
    connect()
    return () => {
      closed = true
      es?.close()
    }
  }, [])

  const bumpTasks = useCallback(() => setTaskVersion((v) => v + 1), [])

  const openChat = useCallback((jid: string, draft?: string) => {
    setRecoOpen(false)
    setSelectedTask(null)
    setTab('chats')
    setSelected(jid)
    setChats((prev) => prev.map((c) => (c.jid === jid ? { ...c, unread_count: 0 } : c)))
    if (draft) setChatDrafts((d) => ({ ...d, [jid]: draft }))
  }, [])

  // Called by MessageThread once it has loaded a draft into its composer,
  // so the draft is single-use and doesn't reappear on re-render.
  const consumeChatDraft = useCallback((jid: string) => {
    setChatDrafts((d) => {
      if (!d[jid]) return d
      const next = { ...d }
      delete next[jid]
      return next
    })
  }, [])

  const openTask = useCallback((id: number) => {
    setRecoOpen(false)
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(id)
    setTab('tasks')
  }, [])

  const openChatTasks = useCallback((jid: string) => {
    setRecoOpen(false)
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(null)
    setTaskCircleFilter(null)
    setTaskChatFilter(jid)
    setTab('tasks')
  }, [])

  const openCircleTasks = useCallback((id: number) => {
    setRecoOpen(false)
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(null)
    setTaskChatFilter(null)
    setTaskCircleFilter(id)
    setTab('tasks')
  }, [])

  const clearTaskFilter = useCallback(() => {
    setTaskChatFilter(null)
    setTaskCircleFilter(null)
    setSelectedTask(null)
  }, [])

  const openContactDM = useCallback(
    (c: Contact) => {
      const jid = c.jid && c.jid.includes('@') ? c.jid : `${c.phone || ''}@s.whatsapp.net`
      setSelectedCircle(null)
      setTab('chats')
      openChat(jid)
    },
    [openChat],
  )

  const openCircle = useCallback((id: number) => {
    setRecoOpen(false)
    setSelected(null)
    setSelectedTask(null)
    setSelectedCircle(id)
  }, [])

  const openReco = useCallback(() => {
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(null)
    setRecoOpen(true)
  }, [])

  return (
    <div className="flex h-screen overflow-hidden bg-neutral-950 text-neutral-100">
      {showSettings && <MediaSettings onClose={() => setShowSettings(false)} />}
      {showProfiling && <ProfilingStatusModal onClose={() => setShowProfiling(false)} />}

      <aside className="flex w-80 shrink-0 flex-col border-r border-neutral-800">
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <div className="flex min-w-0 items-center gap-2">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-emerald-500 text-sm font-bold text-neutral-950">
              W
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold">WhatsApp Bridge</div>
              <div className="truncate text-xs text-neutral-500">
                {device?.push_name || device?.jid || 'Connected'}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <IconButton title="Profiles & AI context" onClick={() => setShowProfiling(true)}>
              🧠
            </IconButton>
            <IconButton title="Media settings" onClick={() => setShowSettings(true)}>
              ⚙
            </IconButton>
            <IconButton title="Log out" onClick={() => api.logout()}>
              ⏻
            </IconButton>
          </div>
        </header>

        <div className="flex border-b border-neutral-800 text-sm">
          <TabButton active={tab === 'chats'} onClick={() => setTab('chats')}>
            Chats
          </TabButton>
          <TabButton active={tab === 'contacts'} onClick={() => setTab('contacts')}>
            Contacts
          </TabButton>
          <TabButton active={tab === 'circles'} onClick={() => setTab('circles')}>
            Circles
          </TabButton>
          <TabButton active={tab === 'tasks'} onClick={() => setTab('tasks')}>
            Tasks
          </TabButton>
        </div>

        {tab === 'chats' && (
          <ChatList chats={chats} nameMap={nameMap} selected={selected} onOpen={openChat} />
        )}
        {tab === 'contacts' && (
          <ContactsPanel
            contacts={contacts}
            activity={activity}
            allTags={tags}
            contactTags={contactTags}
            onTagsChanged={reloadTags}
            onOpen={openContactDM}
          />
        )}
        {tab === 'circles' && (
          <CirclesPanel
            circles={circles}
            selected={selectedCircle}
            recoActive={recoOpen}
            onOpen={openCircle}
            onOpenReco={openReco}
            onChanged={reloadCircles}
            onCreated={(c) => {
              reloadCircles()
              openCircle(c.id)
            }}
          />
        )}
        {tab === 'tasks' && (
          <TasksPanel
            chats={chats}
            nameMap={nameMap}
            chatFilter={taskChatFilter}
            circleFilter={taskCircleFilter}
            circleName={circles.find((c) => c.id === taskCircleFilter)?.name || ''}
            selected={selectedTask}
            version={taskVersion}
            onOpen={openTask}
            onCreated={bumpTasks}
            onClearFilter={clearTaskFilter}
          />
        )}
      </aside>

      <main className="min-w-0 flex-1">
        {recoOpen ? (
          <RecommendationsView onChanged={reloadCircles} onOpenCircle={openCircle} />
        ) : selected ? (
          <MessageThread
            jid={selected}
            chats={chats}
            nameMap={nameMap}
            liveMsg={liveMsg}
            circles={circles}
            allTags={tags}
            contactTags={contactTags}
            initialDraft={chatDrafts[selected] || ''}
            onDraftConsumed={() => consumeChatDraft(selected)}
            onCirclesChanged={reloadCircles}
            onTagsChanged={reloadTags}
            onOpenTask={openTask}
            onTasksChanged={bumpTasks}
            onOpenChatTasks={openChatTasks}
            onSent={(m) => setChats((prev) => bumpChat(prev, m, selectedRef.current))}
          />
        ) : selectedTask != null ? (
          <TaskView
            taskId={selectedTask}
            contacts={contacts}
            circles={circles}
            nameMap={nameMap}
            version={taskVersion}
            onOpenChat={openChat}
            onChanged={bumpTasks}
            onDeleted={() => setSelectedTask(null)}
          />
        ) : selectedCircle != null ? (
          <CircleView
            circleId={selectedCircle}
            circles={circles}
            chats={chats}
            contacts={contacts}
            groups={groups}
            nameMap={nameMap}
            allTags={tags}
            onTagsChanged={reloadTags}
            onOpenChat={openChat}
            onOpenCircle={openCircle}
            onOpenTasks={openCircleTasks}
            onChanged={reloadCircles}
            onDeleted={() => setSelectedCircle(null)}
          />
        ) : (
          <EmptyState />
        )}
      </main>
    </div>
  )
}

// bumpChat moves a chat to the top with the new timestamp; increments unread if
// it is not the currently open chat. Adds the chat if unseen.
function bumpChat(chats: Chat[], m: Message, selected: string | null): Chat[] {
  const preview: ChatPreview = {
    chat_jid: m.chat_jid,
    sender: m.sender,
    sender_name: m.sender_name,
    push_name: m.push_name,
    content: m.content,
    media_type: m.media_type || '',
    media_caption: m.media_caption || '',
    is_from_me: m.is_from_me,
    is_group: m.is_group,
    is_deleted: !!m.is_deleted,
    timestamp: m.timestamp,
  }
  const idx = chats.findIndex((c) => c.jid === m.chat_jid)
  let next = [...chats]
  if (idx >= 0) {
    const c = { ...next[idx] }
    c.last_message_at = m.timestamp
    c.last_message = preview
    if (m.chat_jid !== selected && !m.is_from_me) c.unread_count = (c.unread_count || 0) + 1
    next.splice(idx, 1)
    next.unshift(c)
  } else {
    next.unshift({
      jid: m.chat_jid,
      name: m.chat_name || '',
      chat_type: '',
      last_message_at: m.timestamp,
      unread_count: m.chat_jid !== selected && !m.is_from_me ? 1 : 0,
      is_archived: false,
      is_pinned: false,
      is_muted: false,
      last_message: preview,
    })
  }
  return next
}

function IconButton({
  children,
  title,
  onClick,
}: {
  children: React.ReactNode
  title: string
  onClick: () => void
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className="flex h-8 w-8 items-center justify-center rounded-lg text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-100"
    >
      {children}
    </button>
  )
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={
        'flex-1 py-2.5 font-medium transition ' +
        (active
          ? 'border-b-2 border-emerald-500 text-neutral-100'
          : 'text-neutral-500 hover:text-neutral-300')
      }
    >
      {children}
    </button>
  )
}

function EmptyState() {
  return (
    <div className="flex h-full items-center justify-center text-sm text-neutral-600">
      Select a chat to view messages
    </div>
  )
}

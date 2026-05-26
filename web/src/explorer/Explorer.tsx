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
  type Task,
} from '../api'
import { buildMentionIndex, buildNameMap } from './format'
import { ChatList } from './ChatList'
import { ContactsPanel } from './ContactsPanel'
import { CirclesPanel } from './CirclesPanel'
import { CircleView } from './CircleView'
import { RecommendationsView } from './RecommendationsView'
import { TasksSidebar, type TasksSelection } from './TasksSidebar'
import { TasksView } from './TasksView'
import { TaskView } from './TaskView'
import { MessageThread } from './MessageThread'
import { MediaSettings } from '../Settings'
import { ProfilingStatusModal } from './ProfilingStatus'
import { BriefingModal } from './BriefingView'
import { SearchBar } from './Search'
import { HiddenLockModal } from './HiddenLock'
import { HideChatDialog } from './HideChatDialog'
import { setUnlockToken as setHiddenUnlockToken } from '../hidden'
import { HiddenBadge } from './HiddenBadge'
import { ChatUnlockModal } from './ChatUnlockModal'
import { StarredPanel } from './StarredPanel'
import { CallsPanel } from './CallsPanel'
import { useDesktopNotifications } from '../hooks/useDesktopNotifications'
import { useUnreadBadge } from '../hooks/useUnreadBadge'

type Tab = 'chats' | 'contacts' | 'circles' | 'tasks' | 'calls'

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
  const [taskVersion, setTaskVersion] = useState(0)
  // Sidebar selection for the new Tasks tab. Defaults to "all open".
  const [taskSelection, setTaskSelection] = useState<TasksSelection>({ kind: 'view', view: 'open' })
  // Flat list of every task, used by both the sidebar (for counts) and the
  // main view (so we group/filter client-side). Reloaded when taskVersion bumps.
  const [allTasks, setAllTasks] = useState<Task[]>([])
  const [liveMsg, setLiveMsg] = useState<Message | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [showProfiling, setShowProfiling] = useState(false)
  const [showBriefing, setShowBriefing] = useState(false)
  const [showStarred, setShowStarred] = useState(false)
  // Digit identifiers of the current user — used to color "@you" mention
  // chips in emerald so a ping in a busy group is obvious. WhatsApp's wire
  // format sends LID digits as the mention identifier; we keep the phone
  // form too so older messages and contacts still match.
  const selfDigits = useMemo(() => {
    const out = new Set<string>()
    const addJID = (j?: string) => {
      if (!j) return
      const digits = j.split('@')[0].split(':')[0]
      if (digits) out.add(digits)
    }
    addJID(device?.jid)
    addJID(device?.lid)
    return out
  }, [device?.jid, device?.lid])
  const [showUnlock, setShowUnlock] = useState(false)
  // For the right-click "Hide chat…" flow we open the dialog at the Explorer
  // level (so it works without first opening the chat). Hiding needs no auth.
  const [hideTarget, setHideTarget] = useState<{ jid: string; title: string } | null>(null)
  // pending composer drafts per chat — set by "Nudge" / "Reply" buttons in
  // TaskView, consumed once by MessageThread when it opens that chat.
  const [chatDrafts, setChatDrafts] = useState<Record<string, string>>({})
  // Hidden chats that have been temporarily unlocked via the per-chat
  // fingerprint flow. Indexed by JID — the value is the Chat row fetched
  // from /api/v2/chats/{jid} so MessageThread + the chat header have data
  // to render even though `chats` (the sidebar) doesn't include it.
  const [extraChats, setExtraChats] = useState<Record<string, Chat>>({})
  // Active per-chat unlock prompt. When set, the ChatUnlockModal pops up.
  const [unlockPrompt, setUnlockPrompt] = useState<{ jid: string; name: string } | null>(null)
  // After successful per-chat unlock, openChat is replayed for this JID.
  const pendingOpenRef = useRef<string | null>(null)

  const selectedRef = useRef<string | null>(null)
  selectedRef.current = selected
  // Snapshot of the currently-visible chats — read inside openChat to refuse
  // navigation to JIDs that aren't in the list (e.g. a hidden contact's DM
  // when locked). Mirror via ref so the callback stays stable.
  const chatsRef = useRef<Chat[]>([])
  chatsRef.current = chats

  const nameMap = useMemo(() => buildNameMap(contacts, groups), [contacts, groups])
  const mentionIndex = useMemo(() => buildMentionIndex(contacts), [contacts])

  // Initial loads. Also re-runs whenever the hidden-chats unlock state changes
  // so freshly-unlocked chats appear (or relocked ones disappear).
  useEffect(() => {
    function loadAll() {
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
    }
    loadAll()
    window.addEventListener('wa.unlock-changed', loadAll)
    return () => window.removeEventListener('wa.unlock-changed', loadAll)
  }, [])

  const reloadCircles = useCallback(() => {
    api.circles().then((c) => setCircles(c || [])).catch(() => {})
  }, [])

  // Keep allTasks fresh whenever the Tasks tab is visible or tasks change.
  // Used by the new TasksSidebar (for counts) + TasksView (for the list).
  useEffect(() => {
    if (tab !== 'tasks') return
    api
      .tasks({})
      .then((t) => setAllTasks(t || []))
      .catch(() => setAllTasks([]))
  }, [tab, taskVersion])

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
        // Fire a desktop notification if the OS / user has opted in.
        // All gating (own / muted / focused-current-chat) lives inside.
        fireNotificationRef.current(m)
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
    if (draft) setChatDrafts((d) => ({ ...d, [jid]: draft }))

    // Case 1 — chat is in the visible sidebar list (normal path).
    if (chatsRef.current.some((c) => c.jid === jid)) {
      setRecoOpen(false)
      setSelectedTask(null)
      setTab('chats')
      setSelected(jid)
      setChats((prev) => prev.map((c) => (c.jid === jid ? { ...c, unread_count: 0 } : c)))
      return
    }

    // Case 2 — already temporarily unlocked via the per-chat flow.
    if (extraChats[jid]) {
      setRecoOpen(false)
      setSelectedTask(null)
      setTab('chats')
      setSelected(jid)
      return
    }

    // Case 3 — hidden contact's DM. Open the Touch ID modal; the actual
    // open happens in handleChatUnlocked after the user approves.
    const contact = contacts.find(
      (c) =>
        c.jid === jid ||
        (c.phone && c.phone + '@s.whatsapp.net' === jid) ||
        (c.lid && c.lid + '@lid' === jid),
    )
    if (contact?.is_hidden) {
      const name = contact.name || contact.push_name || contact.business_name || jid
      pendingOpenRef.current = jid
      setUnlockPrompt({ jid, name })
      return
    }

    // Fallthrough: unknown JID we can't route to.
    alert('This chat is locked or not available.')
  }, [contacts, extraChats])

  // Called by ChatUnlockModal once Touch ID succeeded and a per-chat token
  // was stored. We fetch the chat row (now authorised by the new token via
  // the global fetch interceptor) and place it into extraChats so the rest
  // of the UI has data to render. Then we replay the original openChat.
  const handleChatUnlocked = useCallback(async (jid: string) => {
    setUnlockPrompt(null)
    try {
      const chat = await api.chat(jid)
      setExtraChats((prev) => ({ ...prev, [jid]: chat }))
      // Replay openChat for the same JID — extraChats now contains it.
      setRecoOpen(false)
      setSelectedTask(null)
      setTab('chats')
      setSelected(jid)
      pendingOpenRef.current = null
    } catch (e) {
      alert('Could not load chat: ' + (e as Error).message)
    }
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

  const openChatTasks = useCallback((_jid: string) => {
    // From a chat header's "✓ Tasks" — no chat scope in the new layout, so we
    // just open the tasks tab on the default "all open" view.
    setRecoOpen(false)
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(null)
    setTaskSelection({ kind: 'view', view: 'open' })
    setTab('tasks')
  }, [])

  // Tab title + favicon badge driven by the total unread count across all
  // chats (muted ones excluded). Lets the user spot a new message from
  // another tab without focusing this one — same affordance as WA Web.
  useUnreadBadge(chats)

  // Desktop notifications for live incoming messages. Gating, preview
  // formatting, permission + opt-out state all live inside the hook; we
  // just call fire(m) from the SSE handler below and render the optional
  // "Enable notifications" banner at the top of the sidebar.
  const notifications = useDesktopNotifications({
    chats,
    nameMap,
    selectedJid: selected,
    onOpenChat: openChat,
  })
  // Stash fire() in a ref so the SSE useEffect (which subscribes once on
  // mount with empty deps) can always reach the latest closure without
  // re-subscribing the stream.
  const fireNotificationRef = useRef(notifications.fire)
  useEffect(() => { fireNotificationRef.current = notifications.fire }, [notifications.fire])

  const openCircleTasks = useCallback((id: number) => {
    setRecoOpen(false)
    setSelected(null)
    setSelectedCircle(null)
    setSelectedTask(null)
    setTaskSelection({ kind: 'circle', id })
    setTab('tasks')
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
      {showUnlock && (
        <HiddenLockModal
          onUnlocked={() => setShowUnlock(false)}
          onClose={() => setShowUnlock(false)}
        />
      )}
      {unlockPrompt && (
        <ChatUnlockModal
          chatJID={unlockPrompt.jid}
          contactName={unlockPrompt.name}
          onUnlocked={handleChatUnlocked}
          onClose={() => {
            setUnlockPrompt(null)
            pendingOpenRef.current = null
          }}
        />
      )}
      {/* Right-click "Hide chat…" flow rendered here so it works from the list. */}
      {hideTarget && (
        <HideChatDialog
          jid={hideTarget.jid}
          title={hideTarget.title}
          onDone={() => {
            setHideTarget(null)
            // After hide: stay in the normal list view — drop any unlock token
            // so the user doesn't get flipped into the "private mode" view.
            setHiddenUnlockToken(null)
            bumpTasks()
            reloadCircles()
            window.dispatchEvent(new CustomEvent('wa.unlock-changed'))
          }}
          onClose={() => setHideTarget(null)}
        />
      )}
      {showBriefing && (
        <BriefingModal
          onOpenTask={(id) => {
            setShowBriefing(false)
            openTask(id)
          }}
          onOpenChat={(jid) => {
            setShowBriefing(false)
            openChat(jid)
          }}
          onClose={() => setShowBriefing(false)}
        />
      )}
      {showStarred && (
        <StarredPanel
          onOpenChat={(jid) => openChat(jid)}
          onClose={() => setShowStarred(false)}
        />
      )}

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
            <HiddenBadge onClick={() => setShowUnlock(true)} />
            <IconButton title="Starred messages" onClick={() => setShowStarred(true)}>
              ⭐
            </IconButton>
            <IconButton title="Today’s briefing" onClick={() => setShowBriefing(true)}>
              📊
            </IconButton>
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

        {notifications.permission === 'default' && !notifications.dismissed && (
          // One-shot prompt to enable native OS notifications. WA's web
          // client does the same up top. Click "Enable" → browser permission
          // dialog → if granted, alerts start firing on the SSE handler.
          // ✕ persists a dismissed flag so it doesn't keep nagging.
          <div className="flex items-center gap-2 border-b border-neutral-800 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-200">
            <span aria-hidden="true">🔔</span>
            <span className="flex-1">Get notified when a message arrives</span>
            <button
              onClick={notifications.request}
              className="rounded-md bg-emerald-600 px-2.5 py-1 text-[11px] font-medium text-neutral-950 transition hover:bg-emerald-500"
            >
              Enable
            </button>
            <button
              onClick={notifications.dismissBanner}
              title="Dismiss"
              aria-label="Dismiss notification banner"
              className="flex h-6 w-6 items-center justify-center rounded text-emerald-300/80 hover:bg-emerald-500/15 hover:text-emerald-100"
            >
              ✕
            </button>
          </div>
        )}

        <div className="border-b border-neutral-800 px-3 py-2">
          <SearchBar
            onPick={(h) => {
              if (h.kind === 'contact' || h.kind === 'group') openChat(h.id)
              else if (h.kind === 'circle') openCircle(parseInt(h.id, 10))
              else if (h.kind === 'task') openTask(parseInt(h.id, 10))
              else if (h.kind === 'message' && h.chat_jid) openChat(h.chat_jid)
            }}
          />
        </div>

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
          <TabButton active={tab === 'calls'} onClick={() => setTab('calls')}>
            Calls
          </TabButton>
        </div>

        {tab === 'chats' && (
          <ChatList
            chats={chats}
            nameMap={nameMap}
            circles={circles}
            selected={selected}
            onOpen={openChat}
            onRequestHide={(jid, title) => setHideTarget({ jid, title })}
            onChanged={() => {
              window.dispatchEvent(new CustomEvent('wa.unlock-changed'))
              reloadCircles()
            }}
          />
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
          <TasksSidebar
            tasks={allTasks}
            circles={circles}
            ownJID={device?.jid || ''}
            selected={taskSelection}
            onSelect={(s) => {
              setTaskSelection(s)
              setSelectedTask(null) // back to the list when changing scope
            }}
          />
        )}
        {tab === 'calls' && (
          <CallsPanel
            nameMap={nameMap}
            onOpenChat={openChat}
          />
        )}
      </aside>

      <main className="min-w-0 flex-1">
        {recoOpen ? (
          <RecommendationsView onChanged={reloadCircles} onOpenCircle={openCircle} />
        ) : tab === 'tasks' ? (
          // Tasks tab always shows the tasks main view — never the chat/circle
          // that may still be "selected" from another tab. A selected task
          // (clicked from the list) renders detail with a Back button.
          selectedTask != null ? (
            <div className="flex h-full flex-col">
              <button
                onClick={() => setSelectedTask(null)}
                className="self-start px-5 py-2 text-xs text-neutral-400 hover:text-neutral-200"
              >
                ← Back to tasks
              </button>
              <div className="min-h-0 flex-1">
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
              </div>
            </div>
          ) : (
            <TasksView
              tasks={allTasks}
              circles={circles}
              chats={chats}
              nameMap={nameMap}
              ownJID={device?.jid || ''}
              selection={taskSelection}
              onOpenTask={openTask}
              onCreated={bumpTasks}
              onChanged={bumpTasks}
            />
          )
        ) : selected ? (
          <MessageThread
            jid={selected}
            chats={extraChats[selected] ? [...chats, extraChats[selected]] : chats}
            nameMap={nameMap}
            mentionIndex={mentionIndex}
            selfDigits={selfDigits}
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
            onOpenChat={openChat}
            onOpenCircle={openCircle}
            onSent={(m) => setChats((prev) => bumpChat(prev, m, selectedRef.current))}
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

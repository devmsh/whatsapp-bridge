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
import { PrivacySettings } from './PrivacySettings'
import { SelfProfile } from './SelfProfile'
import { StatusUpdatesPanel } from './StatusUpdatesPanel'
import { NewslettersPanel } from './NewslettersPanel'
import { ProfilingStatusModal } from './ProfilingStatus'
import { BriefingModal } from './BriefingView'
import { SearchBar } from './Search'
import { HiddenLockModal } from './HiddenLock'
import { HideChatDialog } from './HideChatDialog'
import { setUnlockToken as setHiddenUnlockToken } from '../hidden'
import { HiddenBadge } from './HiddenBadge'
import { ChatUnlockModal } from './ChatUnlockModal'
import { StarredPanel } from './StarredPanel'
import { QuickRepliesPanel } from './QuickRepliesPanel'
import { WorkingHours } from './WorkingHours'
import { CallsPanel } from './CallsPanel'
import { FocusMode } from './FocusMode'
import { useDesktopNotifications } from '../hooks/useDesktopNotifications'
import { useUnreadBadge } from '../hooks/useUnreadBadge'
import { useScheduledAutopilot } from '../hooks/useScheduledMessages'
import { ShortcutsHelp } from './ShortcutsHelp'
import { NewChatModal } from './NewChatModal'

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
  // Circle currently in full-screen Focus Mode takeover; non-null replaces the
  // entire normal UI (tab bar, aside, main) with <FocusMode>. Set from the
  // per-row "Focus" button in CirclesPanel.
  const [focusCircleId, setFocusCircleId] = useState<number | null>(null)
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
  const [showPrivacy, setShowPrivacy] = useState(false)
  const [showSelfProfile, setShowSelfProfile] = useState(false)
  const [showStatuses, setShowStatuses] = useState(false)
  const [showNewsletters, setShowNewsletters] = useState(false)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showCompose, setShowCompose] = useState(false)
  const [dndOpen, setDndOpen] = useState(false)
  // Message ID the universal search asked us to land on after the chat
  // opens. MessageThread watches it, fires jumpToMessage once the row is
  // in the loaded window, then calls onJumpHandled to clear it so the
  // next chat-switch doesn't re-trigger an old jump.
  const [pendingJumpId, setPendingJumpId] = useState<string | null>(null)
  const [showProfiling, setShowProfiling] = useState(false)
  const [showBriefing, setShowBriefing] = useState(false)
  const [showStarred, setShowStarred] = useState(false)
  const [showQuickReplies, setShowQuickReplies] = useState(false)
  const [showWorkingHours, setShowWorkingHours] = useState(false)
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
    // 'wa.chats-changed' is dispatched by any mutation that doesn't already
    // have a refresh path of its own (e.g. DisappearingSection). It's cheap
    // — the full chats load is ~50 ms locally — and keeps every consumer of
    // the chats prop honest without each one wiring its own onChanged.
    window.addEventListener('wa.chats-changed', loadAll)
    // The composer's quick-reply picker fires this to open the manager
    // panel without prop-drilling through MessageThread.
    const openQuickReplies = () => setShowQuickReplies(true)
    window.addEventListener('wa.open-quick-replies', openQuickReplies)
    return () => {
      window.removeEventListener('wa.unlock-changed', loadAll)
      window.removeEventListener('wa.chats-changed', loadAll)
      window.removeEventListener('wa.open-quick-replies', openQuickReplies)
    }
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
      // Display priority matches WA mobile: a WA-verified business name and
      // the plain business name both beat a self-set push name.
      const name = contact.name || contact.verified_name || contact.business_name || contact.push_name || jid
      pendingOpenRef.current = jid
      setUnlockPrompt({ jid, name })
      return
    }

    // Fallthrough A — looks like a phone-style DM JID we've never chatted
    // with (NewChatModal's "Start chat with +XXX" shortcut, the Saved
    // Messages row, or a deep link). Stub it into extraChats so
    // MessageThread can render an empty thread with the composer; the
    // first send will land in the real chats table and the row will
    // graduate to the sidebar list.
    const phoneMatch = jid.match(/^(\d{6,15})@s\.whatsapp\.net$/)
    if (phoneMatch) {
      // If the target is our own JID (device suffix stripped) label it
      // "Saved messages" instead of the raw "+digits" — same hint WA
      // surfaces for the Message-yourself thread.
      const selfJID = device?.jid?.replace(/:\d+@/, '@')
      const isSelf = selfJID && selfJID === jid
      setExtraChats((prev) =>
        prev[jid]
          ? prev
          : {
              ...prev,
              [jid]: {
                jid,
                name: isSelf ? '⭐ Saved messages' : '+' + phoneMatch[1],
                chat_type: 'dm',
                last_message_at: 0,
                unread_count: 0,
                is_archived: false,
                is_pinned: false,
                is_muted: false,
              },
            },
      )
      setRecoOpen(false)
      setSelectedTask(null)
      setTab('chats')
      setSelected(jid)
      return
    }

    // Fallthrough B: still unknown — we genuinely can't route here.
    alert('This chat is locked or not available.')
  }, [contacts, extraChats, device?.jid])

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
  // Scheduled-message autopilot — polls localStorage every 30 s and fires
  // any messages whose scheduled_at has passed via api.send. Mounted once
  // here at the app root so a single tab does the work even when the user
  // closes the chat (or has it in a different tab).
  useScheduledAutopilot()

  // Persist the last-opened chat across reloads so a browser refresh /
  // crash re-restores the user's spot. localStorage on selected change;
  // mount-time replay below. We deliberately don't track scroll position
  // — MessageThread pins to bottom or the unread divider, which is the
  // sensible landing each time anyway.
  useEffect(() => {
    try {
      if (selected) localStorage.setItem('wa.last-chat-jid', selected)
      else localStorage.removeItem('wa.last-chat-jid')
    } catch {}
  }, [selected])

  // Mount-time replay: once we have a chats snapshot (so openChat can
  // route case-1), reopen the last chat. Guarded so the replay only
  // fires once per page load and never overrides a manually-selected
  // chat (e.g., the user clicked a different row before chats loaded).
  const restoredRef = useRef(false)
  useEffect(() => {
    if (restoredRef.current) return
    if (chats.length === 0) return
    if (selected) {
      restoredRef.current = true
      return
    }
    try {
      const last = localStorage.getItem('wa.last-chat-jid')
      if (last) {
        restoredRef.current = true
        openChat(last)
      }
    } catch {}
  }, [chats, selected, openChat])

  // Global keyboard shortcuts:
  //   ⌘/Ctrl + K          → focus + select the universal search bar
  //   ⌘/Ctrl + /          → open the shortcuts help overlay
  //   ⌘/Ctrl + ⇧ + ]      → next chat in the visible list (WA Web mapping)
  //   ⌘/Ctrl + ⇧ + [      → previous chat in the visible list
  // Cmd+F is handled in MessageThread for in-chat find.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (!(e.metaKey || e.ctrlKey) || e.altKey) return
      const k = e.key.toLowerCase()
      if (!e.shiftKey && k === 'k') {
        e.preventDefault()
        const el = document.getElementById('wa-universal-search') as HTMLInputElement | null
        if (el) {
          el.focus()
          el.select()
        }
        return
      }
      // The "?" character literally produced is typically e.key === '/' (the
      // unmodified key on US layouts) — we want this on Cmd+/ regardless of
      // whether Shift is held, so both ⌘/ and ⌘? open the panel.
      if (k === '/') {
        e.preventDefault()
        setShowShortcuts((v) => !v)
        return
      }
      // Cycle chats with ⌘⇧] (next) / ⌘⇧[ (prev). Some keyboard layouts
      // report these as ']' / '[' on key (Shift is already required by
      // the matchers above); others swap to '}' / '{' with Shift held.
      // Accept both.
      if (e.shiftKey && (k === ']' || k === '}')) {
        e.preventDefault()
        cycleChat(1)
        return
      }
      if (e.shiftKey && (k === '[' || k === '{')) {
        e.preventDefault()
        cycleChat(-1)
        return
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // cycleChat moves the selection to the next or previous chat in the
  // visible normal-mode list. Skips archived + hidden (just like the
  // sidebar) and applies the same pinned-first, recency-second sort
  // ChatList uses, so the keyboard cycle matches what the user sees.
  function cycleChat(direction: -1 | 1) {
    const list = chatsRef.current
      .filter((c) => !c.is_archived && !c.is_hidden)
      .sort((a, b) => {
        const ap = a.is_pinned ? 1 : 0
        const bp = b.is_pinned ? 1 : 0
        if (ap !== bp) return bp - ap
        return (b.last_message_at || 0) - (a.last_message_at || 0)
      })
    if (list.length === 0) return
    const cur = selectedRef.current
    const i = cur ? list.findIndex((c) => c.jid === cur) : -1
    // From "no chat open" go to first (next) / last (prev) so the
    // shortcut always lands somewhere.
    const next =
      i < 0
        ? direction === 1 ? list[0] : list[list.length - 1]
        : list[(i + direction + list.length) % list.length]
    if (next) openChat(next.jid)
  }

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

  // --- Mobile single-pane layout ----------------------------------------
  // The desktop two-pane layout (320px list + conversation) does not fit a
  // phone, so below `md` we show ONE pane: the list, or — once the user
  // opens something — the main pane full-screen with a Back affordance.
  // detailOpen = the main pane is showing navigated-into content.
  const detailOpen =
    recoOpen || selected != null || selectedCircle != null || selectedTask != null
  // The Tasks tab renders its list in <main> (the sidebar holds only the
  // scope picker), so treat that tab as "show main" on mobile too.
  const showMainMobile = detailOpen || tab === 'tasks'
  // Back steps up one level: detail → its list, then list → chats.
  const closeMobileDetail = () => {
    if (selected != null) return setSelected(null)
    if (selectedCircle != null) return setSelectedCircle(null)
    if (recoOpen) return setRecoOpen(false)
    if (selectedTask != null) return setSelectedTask(null)
    if (tab === 'tasks') return setTab('chats')
  }

  if (focusCircleId != null)
    return (
      <FocusMode
        circleId={focusCircleId}
        circles={circles}
        chats={chats}
        nameMap={nameMap}
        onOpenChat={openChat}
        onExit={() => setFocusCircleId(null)}
      />
    )

  return (
    <div className="flex h-screen overflow-hidden bg-neutral-950 text-neutral-100">
      {showSettings && <MediaSettings onClose={() => setShowSettings(false)} />}
      {showPrivacy && <PrivacySettings onClose={() => setShowPrivacy(false)} />}
      {showSelfProfile && <SelfProfile device={device} onClose={() => setShowSelfProfile(false)} />}
      {showStatuses && <StatusUpdatesPanel onClose={() => setShowStatuses(false)} />}
      {showNewsletters && (
        <NewslettersPanel
          onClose={() => setShowNewsletters(false)}
          onOpenChat={(j) => openChat(j)}
        />
      )}
      {showShortcuts && <ShortcutsHelp onClose={() => setShowShortcuts(false)} />}
      {showCompose && (
        <NewChatModal
          contacts={contacts}
          groups={groups}
          selfDevice={device}
          onPick={(jid) => openChat(jid)}
          onClose={() => setShowCompose(false)}
        />
      )}
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
      {showQuickReplies && <QuickRepliesPanel onClose={() => setShowQuickReplies(false)} />}
      {showWorkingHours && <WorkingHours onClose={() => setShowWorkingHours(false)} />}

      <aside
        className={
          'w-full shrink-0 flex-col border-r border-neutral-800 md:flex md:w-80 ' +
          (showMainMobile ? 'hidden' : 'flex')
        }
      >
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
          {/* Header cluster — 11 actions don't fit in the 320 px sidebar.
              Keep the 5 most-tapped visible as SVG icon buttons (matches
              the rest of the app — emoji glyphs render inconsistently
              across OSes), fold the rest into a "⋮ More" overflow menu.
              HiddenBadge + DndButton stay outside because they have their
              own per-state rendering. */}
          <div className="flex items-center gap-0.5">
            <HiddenBadge onClick={() => setShowUnlock(true)} />
            <IconButton title="New chat" onClick={() => setShowCompose(true)}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4z" />
              </svg>
            </IconButton>
            <DndButton
              dndUntil={notifications.dndUntil}
              setDndUntil={notifications.setDndUntil}
              open={dndOpen}
              setOpen={setDndOpen}
            />
            <IconButton title="Starred messages" onClick={() => setShowStarred(true)}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true">
                <path d="M12 2 14.78 8.63 22 9.24l-5.5 4.73L18.18 21 12 17.27 5.82 21l1.68-7.03L2 9.24l7.22-.61L12 2z" />
              </svg>
            </IconButton>
            <IconButton title="Status updates" onClick={() => setShowStatuses(true)}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="9" />
                <circle cx="12" cy="12" r="3" />
              </svg>
            </IconButton>
            <IconButton title="Channels" onClick={() => setShowNewsletters(true)}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 11l18-5v12L3 13v-2z" />
                <path d="M11.6 16.8a3 3 0 1 1-5.8-1.6" />
              </svg>
            </IconButton>
            <MoreMenu
              items={[
                {
                  label: 'Your profile',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                      <circle cx="12" cy="7" r="4" />
                    </svg>
                  ),
                  onClick: () => setShowSelfProfile(true),
                },
                {
                  label: 'Privacy',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <rect x="3" y="11" width="18" height="11" rx="2" />
                      <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                    </svg>
                  ),
                  onClick: () => setShowPrivacy(true),
                },
                {
                  label: "Today's briefing",
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <line x1="18" y1="20" x2="18" y2="10" />
                      <line x1="12" y1="20" x2="12" y2="4" />
                      <line x1="6" y1="20" x2="6" y2="14" />
                    </svg>
                  ),
                  onClick: () => setShowBriefing(true),
                },
                {
                  label: 'Profiles & AI context',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M12 2a5 5 0 0 0-5 5c0 1.5.7 2.8 1.8 3.7C7.1 11.7 6 13.7 6 16v3h12v-3c0-2.3-1.1-4.3-2.8-5.3A5 5 0 0 0 17 7a5 5 0 0 0-5-5z" />
                    </svg>
                  ),
                  onClick: () => setShowProfiling(true),
                },
                {
                  label: 'Media settings',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <circle cx="12" cy="12" r="3" />
                      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
                    </svg>
                  ),
                  onClick: () => setShowSettings(true),
                },
                {
                  label: 'Quick replies',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M3 11.5a8.38 8.38 0 0 1 8.5-8.5 8.5 8.5 0 0 1 8.5 8.5 8.38 8.38 0 0 1-8.5 8.5 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8z" />
                    </svg>
                  ),
                  onClick: () => setShowQuickReplies(true),
                },
                {
                  label: 'Working hours',
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <circle cx="12" cy="12" r="10" />
                      <polyline points="12 6 12 12 16 14" />
                    </svg>
                  ),
                  onClick: () => setShowWorkingHours(true),
                },
                { divider: true },
                {
                  label: 'Log out',
                  destructive: true,
                  icon: (
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                      <polyline points="16 17 21 12 16 7" />
                      <line x1="21" y1="12" x2="9" y2="12" />
                    </svg>
                  ),
                  onClick: () => api.logout(),
                },
              ]}
            />
          </div>
        </header>

        {notifications.permission === 'default' && !notifications.dismissed && (
          // One-shot prompt to enable native OS notifications. WA's web
          // client does the same up top. Click "Enable" → browser permission
          // dialog → if granted, alerts start firing on the SSE handler.
          // ✕ persists a dismissed flag so it doesn't keep nagging.
          <div className="flex items-center gap-2 border-b border-neutral-800 bg-neutral-900/60 px-3 py-2 text-xs text-neutral-300">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="shrink-0 text-neutral-400">
              <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
              <path d="M13.73 21a2 2 0 0 1-3.46 0" />
            </svg>
            <span className="flex-1 leading-snug">Get notified about new messages</span>
            <button
              onClick={notifications.request}
              className="shrink-0 rounded-md px-2 py-1 text-[11px] font-medium text-emerald-300 transition hover:bg-emerald-500/15"
            >
              Enable
            </button>
            <button
              onClick={notifications.dismissBanner}
              title="Dismiss"
              aria-label="Dismiss notification banner"
              className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
            >
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M18 6 6 18M6 6l12 12" />
              </svg>
            </button>
          </div>
        )}

        <div className="border-b border-neutral-800 px-3 py-2">
          <SearchBar
            onPick={(h) => {
              if (h.kind === 'contact' || h.kind === 'group') openChat(h.id)
              else if (h.kind === 'circle') openCircle(parseInt(h.id, 10))
              else if (h.kind === 'task') openTask(parseInt(h.id, 10))
              else if (h.kind === 'message' && h.chat_jid) {
                // Open the chat, then ask MessageThread to scroll to the
                // exact message id. The thread fires jumpToMessage once
                // the row lands in the loaded window — same flash as
                // tapping a quoted-reply chip.
                openChat(h.chat_jid)
                setPendingJumpId(h.id)
              }
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
            onFocusCircle={setFocusCircleId}
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

      <main
        className={
          'min-w-0 flex-1 flex-col ' + (showMainMobile ? 'flex' : 'hidden md:flex')
        }
      >
        {/* Mobile-only back bar: returns to the list pane. Hidden on md+. */}
        {showMainMobile && (
          <button
            onClick={closeMobileDetail}
            className="flex shrink-0 items-center gap-1.5 border-b border-neutral-800 px-3 py-2.5 text-sm text-neutral-300 transition hover:text-neutral-100 md:hidden"
          >
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M19 12H5M12 19l-7-7 7-7" />
            </svg>
            Back
          </button>
        )}
        <div className="min-h-0 flex-1">
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
            pendingJumpId={pendingJumpId}
            onJumpHandled={() => setPendingJumpId(null)}
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
        </div>
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

// MoreMenu is the kebab (⋮) overflow inside the sidebar header. Holds the
// less-frequently-tapped actions (Profile, Privacy, Settings, …) so the
// row fits in the 320 px sidebar without wrapping.
type MoreItem =
  | { divider: true }
  | {
      label: string
      icon: React.ReactNode
      onClick: () => void
      destructive?: boolean
      divider?: undefined
    }
function MoreMenu({ items }: { items: MoreItem[] }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        title="More"
        aria-label="More"
        aria-expanded={open}
        className={
          'flex h-8 w-8 items-center justify-center rounded-lg transition ' +
          (open
            ? 'bg-neutral-800 text-neutral-100'
            : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100')
        }
      >
        {/* Vertical 3-dot kebab */}
        <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true">
          <circle cx="12" cy="5" r="1.6" />
          <circle cx="12" cy="12" r="1.6" />
          <circle cx="12" cy="19" r="1.6" />
        </svg>
      </button>
      {open && (
        <>
          {/* Click-away catcher dismisses on any outside click. */}
          <div
            onClick={() => setOpen(false)}
            className="fixed inset-0 z-30"
            aria-hidden="true"
          />
          <div className="absolute right-0 top-full z-40 mt-1 w-52 overflow-hidden rounded-lg border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60">
            {items.map((it, i) =>
              it.divider ? (
                <div key={'d' + i} className="my-1 h-px bg-neutral-800" />
              ) : (
                <button
                  key={it.label}
                  onClick={() => {
                    it.onClick()
                    setOpen(false)
                  }}
                  className={
                    'flex w-full items-center gap-2.5 px-3 py-2 text-left text-xs transition hover:bg-neutral-800 ' +
                    (it.destructive ? 'text-red-300' : 'text-neutral-200')
                  }
                >
                  <span
                    className={
                      'flex h-5 w-5 shrink-0 items-center justify-center ' +
                      (it.destructive ? 'text-red-300' : 'text-neutral-400')
                    }
                  >
                    {it.icon}
                  </span>
                  {it.label}
                </button>
              ),
            )}
          </div>
        </>
      )}
    </div>
  )
}

// DndButton is the 🌙 Do Not Disturb toggle in the sidebar header. Tap to
// open a small popover of duration choices (1h / 8h / Until tomorrow);
// pick one and notifications + the audio ding both go silent until the
// deadline. While DND is on the icon swaps to amber + the title shows
// the time it ends so the user knows what they've committed to. Tapping
// it again while active turns DND off immediately.
function DndButton({
  dndUntil,
  setDndUntil,
  open,
  setOpen,
}: {
  dndUntil: number
  setDndUntil: (ts: number) => void
  open: boolean
  setOpen: (v: boolean) => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open, setOpen])

  const active = dndUntil > 0 && dndUntil > Math.floor(Date.now() / 1000)
  const title = active
    ? `Do Not Disturb · until ${new Date(dndUntil * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} — tap to turn off`
    : 'Do Not Disturb'

  function set(hours: number) {
    if (hours <= 0) {
      setDndUntil(0)
    } else {
      const end = Math.floor(Date.now() / 1000) + Math.round(hours * 3600)
      setDndUntil(end)
    }
    setOpen(false)
  }

  // "Until tomorrow" = end of today (23:59:59 local). If the user picks it
  // late at night they probably mean "until I wake up tomorrow" — close
  // enough; we'd need an explicit wake-time UI to do better.
  function untilTomorrow() {
    const d = new Date()
    d.setHours(23, 59, 59, 999)
    setDndUntil(Math.floor(d.getTime() / 1000))
    setOpen(false)
  }

  return (
    <div ref={ref} className="relative">
      <button
        title={title}
        onClick={() => {
          // Toggle off in one click if already active; otherwise open the
          // duration picker so the user sets a deadline.
          if (active) set(0)
          else setOpen(!open)
        }}
        className={
          'flex h-8 w-8 items-center justify-center rounded-lg transition ' +
          (active
            ? 'bg-amber-500/20 text-amber-300 hover:bg-amber-500/30'
            : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100')
        }
      >
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      </button>
      {open && !active && (
        <div className="absolute right-0 top-full z-40 mt-1 w-44 rounded-xl border border-neutral-700 bg-neutral-900 py-1 text-sm shadow-2xl shadow-black/60">
          <button onClick={() => set(1)} className="block w-full px-3 py-1.5 text-left text-neutral-200 transition hover:bg-neutral-800">
            For 1 hour
          </button>
          <button onClick={() => set(8)} className="block w-full px-3 py-1.5 text-left text-neutral-200 transition hover:bg-neutral-800">
            For 8 hours
          </button>
          <button onClick={untilTomorrow} className="block w-full px-3 py-1.5 text-left text-neutral-200 transition hover:bg-neutral-800">
            Until tomorrow
          </button>
        </div>
      )}
    </div>
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
    <div className="flex h-full flex-col items-center justify-center gap-5 px-6 text-center">
      <div className="flex h-20 w-20 items-center justify-center rounded-full bg-neutral-900 text-neutral-600 ring-1 ring-neutral-800">
        <svg viewBox="0 0 24 24" width="36" height="36" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" />
        </svg>
      </div>
      <div className="space-y-1.5">
        <h2 className="text-base font-semibold text-neutral-200">Your messages</h2>
        <p className="mx-auto max-w-xs text-sm leading-relaxed text-neutral-500">
          Select a chat to read and reply. Your conversations stay in sync with your phone.
        </p>
      </div>
    </div>
  )
}

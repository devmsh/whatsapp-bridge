import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api, type Chat, type Circle, type GroupParticipant, type Message, type PresenceEntry, type Tag } from '../api'
import {
  chatTitle, dayLabel, isGroup, isNewsletter, isStatus, jidUser, mediaURL, senderTitle,
  type MentionEntry,
} from './format'
import { senderColor } from './colors'
import { MessageBubble } from './MessageBubble'
import { ChatCircles } from './ChatCircles'
import { TagChips, TagEditor } from './Tags'
import { ExtractionsModal } from './Extractions'
import { ProfileCard } from './ProfileCard'
import { ChatAvatar } from './ChatAvatar'
import { DraftRepliesPopover } from './DraftReplies'
import { DashboardModal } from './Dashboard'
import { HideChatDialog } from './HideChatDialog'
import { setUnlockToken } from '../hidden'
import { ImageLightbox, type LightboxImage } from './ImageLightbox'
import { ForwardPicker } from './ForwardPicker'
import { EmojiPicker } from './EmojiPicker'

const PAGE = 100

// MessageThread loads and renders the conversation for one chat, appends live
// messages, and supports loading earlier history.
export function MessageThread({
  jid,
  chats,
  nameMap,
  mentionIndex,
  selfDigits,
  liveMsg,
  circles,
  allTags,
  contactTags,
  initialDraft = '',
  onDraftConsumed,
  onCirclesChanged,
  onTagsChanged,
  onOpenTask,
  onTasksChanged,
  onOpenChatTasks,
  onOpenChat,
  onOpenCircle,
  onSent,
}: {
  jid: string
  chats: Chat[]
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  /** Digit identifiers of the current user — bubbles use this to highlight
   *  @-mention chips that ping you (and add a faint emerald ring on the
   *  bubble itself when you've been mentioned). */
  selfDigits?: Set<string>
  liveMsg: Message | null
  circles: Circle[]
  allTags: Tag[]
  contactTags: Record<string, Tag[]>
  initialDraft?: string
  onDraftConsumed?: () => void
  onCirclesChanged: () => void
  onTagsChanged: () => void
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChatTasks: (jid: string) => void
  onOpenChat?: (jid: string) => void
  onOpenCircle?: (id: number) => void
  onSent?: (m: Message) => void
}) {
  const [messages, setMessages] = useState<Message[]>([])
  const [limit, setLimit] = useState(PAGE)
  const [loading, setLoading] = useState(true)
  const [hasMore, setHasMore] = useState(false)
  const [extracting, setExtracting] = useState(false)
  const [showHistory, setShowHistory] = useState(false)
  const [liveRunId, setLiveRunId] = useState<string | null>(null)
  const [showDrafts, setShowDrafts] = useState(false)
  const [composerDraft, setComposerDraft] = useState('')
  const [showDashboard, setShowDashboard] = useState(false)
  const [showHideDialog, setShowHideDialog] = useState(false)
  const [replyTo, setReplyTo] = useState<Message | null>(null)
  // The message currently being edited (your own text bubble, inside WA's
  // 15-minute window). Setting it puts the Composer into edit mode: textarea
  // pre-filled with the original body, a pencil-marked chip above, and Send
  // routes through api.edit instead of api.send. Mutually exclusive with
  // replyTo — picking one clears the other.
  const [editingMsg, setEditingMsg] = useState<Message | null>(null)
  // null = closed. Index into lightboxImages when the user clicks an image.
  const [lightboxIdx, setLightboxIdx] = useState<number | null>(null)
  // null = closed. The message staged for forwarding to N other chats.
  const [forwardMsg, setForwardMsg] = useState<Message | null>(null)
  // Whether a file is currently being dragged over the thread area — drives
  // the "Drop to send" overlay. Drag enter/leave events fire on every child
  // so we count them to avoid flicker.
  const [dragging, setDragging] = useState(false)
  const dragDepth = useRef(0)
  // Callback ref the Composer hands us on mount so we can push a File into
  // its attachment slot when the user drops one on the thread.
  const composerSetAttachment = useRef<((f: File) => void) | null>(null)

  // Build the carousel from every downloaded image in the current message
  // window. Sender labels match the rest of the thread (per-sender color is
  // handled inside the lightbox via the sender string — color is bubble-only).
  const lightboxImages: LightboxImage[] = useMemo(() => {
    const out: LightboxImage[] = []
    for (const m of messages) {
      if (m.media_type !== 'image' || !m.media_path) continue
      const url = mediaURL(m.media_path, m.chat_jid)
      if (!url) continue
      out.push({
        id: m.id,
        url,
        caption: m.media_caption,
        sender: m.is_from_me
          ? 'You'
          : senderTitle(m.sender, m.sender_name, m.push_name, nameMap),
        timestamp: m.timestamp,
      })
    }
    return out
  }, [messages, nameMap])

  function openLightboxFor(msg: Message) {
    const i = lightboxImages.findIndex((img) => img.id === msg.id)
    if (i >= 0) setLightboxIdx(i)
  }
  const scrollRef = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)
  // Mirror of stickToBottom as React state, so the floating scroll-to-bottom
  // FAB can render conditionally. We keep the ref alongside because the
  // useLayoutEffect that pins the view runs synchronously and would race a
  // setState.
  const [atBottom, setAtBottom] = useState(true)
  // Number of incoming messages that arrived while the user was scrolled up
  // — drives the small badge on the FAB, exactly like WhatsApp's "↓ 3" pill.
  // Own messages don't count; sending one already snaps the view to bottom.
  const [unreadBelow, setUnreadBelow] = useState(0)
  // In-chat search state. The magnifying-glass in the header toggles the
  // search bar; query filters the currently-loaded message window;
  // matchIndex steps through hits via ↑/↓ / Enter. Mirrors WA's per-chat
  // search UX — closed by Esc or the ✕ button.
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchIdx, setSearchIdx] = useState(0)
  // Transient "flash this message" state — set by jumpToMessage when the
  // user clicks a quoted-reply preview, auto-cleared after a brief moment.
  // Visualized the same way as the search highlight (amber ring on the
  // matching bubble) but lives in a separate state so a search-in-progress
  // doesn't get permanently overridden by a one-shot jump.
  const [flashId, setFlashId] = useState<string | null>(null)
  const flashTimer = useRef<number | null>(null)
  // Snapshot of unread_count taken when we first open this chat — drives
  // the "X unread messages" divider that splits the timeline between read
  // and unread. We freeze it here so the divider stays put even as the
  // bridge re-fetches the chat list and unread_count starts climbing again
  // (or drops to 0 because we marked-read). Cleared on every chat switch.
  const initialUnreadRef = useRef<number>(0)

  const chat = chats.find((c) => c.jid === jid)
  const title = chat ? chatTitle(chat, nameMap) : '+' + jidUser(jid)
  const group = isGroup(jid)
  const isContact = !group && !isStatus(jid) && !isNewsletter(jid)
  // Subscribe + poll presence for DMs. presenceLine is '' when there's
  // nothing fresh to show (privacy-hidden, stale, or not yet learned).
  const presence = useDmPresence(isContact ? jid : null)
  const presenceLine = presence ? formatPresence(presence) : ''
  // For groups, poll the typing cache so we can render WA's
  // "X is typing…" / "X and Y are typing…" / "Several people are typing…"
  // header line. Resolved to display names via nameMap.
  const groupTyping = useGroupTyping(group ? jid : null)
  const groupTypingLine = group ? formatGroupTyping(groupTyping, nameMap) : ''

  // Load when the chat or page size changes.
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    api
      .messages(jid, limit)
      .then((msgs) => {
        if (cancelled) return
        setMessages(msgs || [])
        setHasMore((msgs?.length || 0) >= limit)
        setLoading(false)
      })
      .catch(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [jid, limit])

  // Reset page size + clear any per-message UI when switching chats.
  useEffect(() => {
    setLimit(PAGE)
    stickToBottom.current = true
    setAtBottom(true)
    setUnreadBelow(0)
    setReplyTo(null)
    setEditingMsg(null)
    setLightboxIdx(null)
    setForwardMsg(null)
    setSearchOpen(false)
    setSearchQuery('')
    setSearchIdx(0)
    setFlashId(null)
    if (flashTimer.current) {
      window.clearTimeout(flashTimer.current)
      flashTimer.current = null
    }
    // Snapshot the chat's unread count for the divider. `chats` may still
    // be loading (chat undefined → 0), in which case no divider — same as
    // WA when there's nothing unread.
    initialUnreadRef.current = chats.find((c) => c.jid === jid)?.unread_count ?? 0
  }, [jid])

  // Find the boundary where the "X unread messages" divider goes. We walk
  // messages newest → oldest, counting incoming (not is_from_me) messages
  // until we've reached the snapshotted unread_count; the divider lands
  // right before that message — i.e. between read history and the first
  // unread reply. Capped at 50 so a chat that's been ignored for weeks
  // doesn't render an absurd "327 unread messages" line.
  //
  // When the snapshot is 0 (chat was already caught up when opened, or
  // chats hadn't loaded yet) we render nothing — same as WA.
  const unreadDivider = useMemo<{ beforeId: string; count: number } | null>(() => {
    const target = Math.min(initialUnreadRef.current, 50)
    if (target <= 0 || messages.length === 0) return null
    let count = 0
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i]
      if (m.is_from_me) continue
      count++
      if (count === target) return { beforeId: m.id, count: target }
    }
    return null
  }, [messages])

  // Build the ordered list of message IDs that match the search query.
  // Search runs only on the currently-loaded window (no backend round-trip
  // for this v1); use "Load earlier messages" to widen the haystack.
  const matchIds = useMemo<string[]>(() => {
    const q = searchQuery.trim().toLowerCase()
    if (!q) return []
    const out: string[] = []
    for (const m of messages) {
      if (m.is_deleted) continue
      const hay = ((m.content || '') + ' ' + (m.media_caption || '')).toLowerCase()
      if (hay.includes(q)) out.push(m.id)
    }
    return out
  }, [messages, searchQuery])

  // Clamp the active index whenever the match list shrinks.
  useEffect(() => {
    if (searchIdx >= matchIds.length) setSearchIdx(Math.max(0, matchIds.length - 1))
  }, [matchIds.length, searchIdx])

  // Scroll the active match into view as it changes. Uses data-msg-id on
  // the row wrappers Timeline emits — cheap querySelector, no prop drilling.
  const activeMatchId = matchIds[searchIdx]
  useEffect(() => {
    if (!activeMatchId) return
    const root = scrollRef.current
    if (!root) return
    const el = root.querySelector(`[data-msg-id="${CSS.escape(activeMatchId)}"]`) as HTMLElement | null
    if (el) {
      stickToBottom.current = false
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [activeMatchId])

  // Append a live message that belongs to this chat. If the user is scrolled
  // up reading older history, bump the unread-below counter so the FAB's
  // badge climbs (WA's "↓ 3" pill). Own messages never count toward it —
  // sending one snaps the view to bottom in handleSent.
  useEffect(() => {
    if (!liveMsg || liveMsg.chat_jid !== jid) return
    setMessages((prev) => {
      if (prev.some((m) => m.id === liveMsg.id)) return prev
      return [...prev, liveMsg]
    })
    if (!stickToBottom.current && !liveMsg.is_from_me) {
      setUnreadBelow((n) => n + 1)
    }
  }, [liveMsg, jid])

  // Keep the view pinned to the newest message after loads/appends.
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight
  }, [messages, loading])

  function onScroll() {
    const el = scrollRef.current
    if (!el) return
    const nowAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    stickToBottom.current = nowAtBottom
    // Only re-render when the boundary flips, not on every scroll tick.
    setAtBottom((prev) => {
      if (prev === nowAtBottom) return prev
      // Bottom reached → clear the unread counter; the user has caught up.
      if (nowAtBottom) setUnreadBelow(0)
      return nowAtBottom
    })
  }

  // Smooth scroll to the newest message + reset the unread-below counter.
  // Called from the floating ↓ FAB.
  function scrollToBottom() {
    const el = scrollRef.current
    if (!el) return
    stickToBottom.current = true
    el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    setAtBottom(true)
    setUnreadBelow(0)
  }

  function loadEarlier() {
    stickToBottom.current = false
    setAtBottom(false)
    setLimit((l) => l + PAGE)
  }

  // Optimistic star/unstar: flip is_starred in the local message immediately
  // so the bubble's footer ⭐ + the hover button update before the bridge
  // round-trip. Roll back if /star or /unstar errors.
  async function handleStar(target: Message, next: boolean) {
    setMessages((prev) =>
      prev.map((m) => (m.id === target.id ? { ...m, is_starred: next } : m)),
    )
    try {
      if (next) await api.star(jid, target.id)
      else await api.unstar(jid, target.id)
    } catch (e) {
      setMessages((prev) =>
        prev.map((m) => (m.id === target.id ? { ...m, is_starred: !next } : m)),
      )
      console.warn('star toggle failed:', e)
    }
  }

  // Apply my own reaction optimistically: hit /api/v2/react and append a
  // chip below the bubble immediately, replacing any previous reaction I had
  // on that message. WhatsApp treats reactions as self-replacing per user.
  async function handleReact(target: Message, emoji: string) {
    setMessages((prev) =>
      prev.map((m) => {
        if (m.id !== target.id) return m
        const others = (m.reactions || []).filter((r) => r.sender !== '__me__')
        const next = emoji
          ? [
              ...others,
              {
                message_id: m.id,
                chat_jid: m.chat_jid,
                sender: '__me__',
                sender_name: 'You',
                emoji,
                timestamp: Math.floor(Date.now() / 1000),
              },
            ]
          : others
        return { ...m, reactions: next }
      }),
    )
    try {
      await api.react(jid, target.id, emoji)
    } catch (e) {
      // Roll the optimistic edit back if the bridge rejected it.
      setMessages((prev) =>
        prev.map((m) => (m.id === target.id ? { ...m, reactions: target.reactions } : m)),
      )
      console.warn('react failed:', e)
    }
  }

  // Switch the Composer into edit mode for one of your own bubbles. Reply
  // and edit are mutually exclusive (the chip slot is shared and WA does the
  // same), so picking edit clears any in-flight reply target.
  function startEdit(target: Message) {
    setReplyTo(null)
    setEditingMsg(target)
  }

  // Jump to a specific message in the thread (used when the user clicks a
  // quoted-reply preview to find the original). Reuses the same data-msg-id
  // anchor + amber ring the search uses, but auto-clears after 1.6s so the
  // jump feels like a quick flash rather than a sticky highlight. Silently
  // no-ops if the message isn't in the loaded window — WA's "Load earlier"
  // is the escape hatch.
  function jumpToMessage(id: string) {
    const root = scrollRef.current
    if (!root) return
    const el = root.querySelector(`[data-msg-id="${CSS.escape(id)}"]`) as HTMLElement | null
    if (!el) return
    stickToBottom.current = false
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    setFlashId(id)
    if (flashTimer.current) window.clearTimeout(flashTimer.current)
    flashTimer.current = window.setTimeout(() => {
      setFlashId(null)
      flashTimer.current = null
    }, 1600)
  }

  // Called by the Composer after api.edit succeeds. Mutates the local bubble
  // so the new text + the small italic "edited" marker appear right away,
  // without waiting for a refetch.
  function applyLocalEdit(messageID: string, newText: string) {
    setMessages((prev) =>
      prev.map((m) =>
        m.id === messageID
          ? { ...m, content: newText, is_edit: true }
          : m,
      ),
    )
  }

  // Append a just-sent message locally. Sent messages go straight through the
  // send API and are not echoed over the SSE stream, so we add them here.
  // Sending always snaps to bottom — matches WA: hitting Enter scrolls you
  // back to the newest message even if you were reading history.
  function handleSent(m: Message) {
    stickToBottom.current = true
    setAtBottom(true)
    setUnreadBelow(0)
    setMessages((prev) => (prev.some((p) => p.id === m.id) ? prev : [...prev, m]))
    onSent?.(m)
  }

  const canSend = !isStatus(jid) && !isNewsletter(jid)

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-neutral-800 px-4 py-3">
        <button
          onClick={() => setShowDashboard(true)}
          title="See everything about this chat"
          className="transition hover:opacity-80"
        >
          <ChatAvatar jid={jid} title={title} group={group} size={36} />
        </button>
        <div className="min-w-0 flex-1">
          <div dir="auto" className="truncate text-sm font-semibold">
            {title}
          </div>
          <div
            className={
              'truncate text-xs ' +
              // Highlight typing in emerald (same accent as own bubbles) so
              // 'typing…' actually pops the way it does in official WA.
              ((group ? groupTypingLine : presenceLine === 'typing…')
                ? 'text-emerald-400'
                : 'text-neutral-500')
            }
          >
            {group
              ? groupTypingLine || 'Group'
              : presenceLine || jid.replace('@s.whatsapp.net', '')}
          </div>
          {isContact && (contactTags[jid]?.length ?? 0) > 0 && (
            <div className="mt-1">
              <TagChips tags={contactTags[jid] || []} />
            </div>
          )}
        </div>
        {isContact && (
          <TagEditor
            jid={jid}
            tags={contactTags[jid] || []}
            allTags={allTags}
            onChanged={onTagsChanged}
          />
        )}
        {group && (
          <button
            onClick={async () => {
              if (extracting) return
              setExtracting(true)
              try {
                const r = await api.extractTasks(jid, title)
                setLiveRunId(r.run_id)
                setShowHistory(true)
              } catch (e) {
                alert('Extraction failed to start: ' + (e as Error).message)
              } finally {
                setExtracting(false)
              }
            }}
            disabled={extracting}
            className="shrink-0 rounded-lg bg-emerald-500/15 px-2.5 py-1.5 text-xs font-medium text-emerald-300 transition hover:bg-emerald-500/25 disabled:opacity-60"
            title="Extract tasks from this group with AI"
          >
            {extracting ? 'Extracting…' : '✨ Extract tasks'}
          </button>
        )}
        {group && (
          <button
            onClick={() => setShowHistory(true)}
            className="shrink-0 rounded-lg border border-neutral-700 px-2.5 py-1.5 text-xs text-neutral-300 transition hover:bg-neutral-800"
            title="See past extraction runs and what the agent did"
          >
            🕘 History
          </button>
        )}
        <button
          onClick={() => setShowDrafts(true)}
          className="shrink-0 rounded-lg bg-sky-500/15 px-2.5 py-1.5 text-xs font-medium text-sky-300 transition hover:bg-sky-500/30"
          title="Draft 2-3 candidate replies (AI)"
        >
          ✨ Draft
        </button>
        {chat?.is_hidden ? (
          <button
            onClick={async () => {
              await api.unhideChat(jid)
              // Tell the rest of the UI the locked set changed; chat list
              // refetches → now-unhidden chat moves out of "private mode".
              window.dispatchEvent(new CustomEvent('wa.unlock-changed'))
            }}
            className="shrink-0 rounded-lg border border-emerald-700 px-2.5 py-1.5 text-xs font-medium text-emerald-300 transition hover:bg-emerald-500/15"
            title="Unhide this chat — it will return to your main list"
          >
            🔓 Unhide
          </button>
        ) : (
          <button
            onClick={() => setShowHideDialog(true)}
            className="shrink-0 rounded-lg border border-neutral-700 px-2.5 py-1.5 text-xs text-neutral-400 transition hover:bg-neutral-800"
            title="Hide this chat (and delete its AI-derived data)"
          >
            🔒
          </button>
        )}
        <button
          onClick={() => onOpenChatTasks(jid)}
          className="shrink-0 rounded-lg border border-neutral-700 px-2.5 py-1.5 text-xs text-neutral-300 transition hover:bg-neutral-800"
          title="Tasks in this chat"
        >
          ✓ Tasks
        </button>
        <button
          onClick={() => setSearchOpen((v) => !v)}
          title="Search in this chat (Cmd-F)"
          aria-label="Search in this chat"
          className={
            'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition ' +
            (searchOpen
              ? 'border-emerald-600/60 bg-emerald-500/15 text-emerald-300'
              : 'border-neutral-700 text-neutral-300 hover:bg-neutral-800')
          }
        >
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="7" />
            <path d="m21 21-4.3-4.3" />
          </svg>
        </button>
        <ChatCircles jid={jid} circles={circles} onChanged={onCirclesChanged} />
      </header>

      {(group || isContact) && <ProfileCard type={group ? 'group' : 'contact'} ref_={jid} />}

      {searchOpen && (
        <SearchBar
          query={searchQuery}
          onQueryChange={(q) => { setSearchQuery(q); setSearchIdx(0) }}
          matchCount={matchIds.length}
          activeIdx={searchIdx}
          onPrev={() => setSearchIdx((i) => (matchIds.length ? (i - 1 + matchIds.length) % matchIds.length : 0))}
          onNext={() => setSearchIdx((i) => (matchIds.length ? (i + 1) % matchIds.length : 0))}
          onClose={() => { setSearchOpen(false); setSearchQuery(''); setSearchIdx(0) }}
        />
      )}

      {/* Scroll wrapper: stays `relative` so the floating ↓ FAB + the drop
          overlay anchor to it (and don't scroll out of view) while the inner
          div handles all the actual scrolling. */}
      <div
        className="relative min-h-0 flex-1"
        // Drag-and-drop: any file dropped onto the thread becomes the
        // composer's next attachment (same staging path as paperclip /
        // paste from loop #7). We count enter/leave depth so dragging
        // over child elements doesn't flicker the overlay.
        onDragEnter={(e) => {
          if (!canSend) return
          if (!Array.from(e.dataTransfer.types).includes('Files')) return
          dragDepth.current += 1
          setDragging(true)
        }}
        onDragOver={(e) => {
          if (canSend && Array.from(e.dataTransfer.types).includes('Files')) {
            e.preventDefault()
            e.dataTransfer.dropEffect = 'copy'
          }
        }}
        onDragLeave={() => {
          if (!canSend) return
          dragDepth.current = Math.max(0, dragDepth.current - 1)
          if (dragDepth.current === 0) setDragging(false)
        }}
        onDrop={(e) => {
          if (!canSend) return
          dragDepth.current = 0
          setDragging(false)
          const file = e.dataTransfer.files?.[0]
          if (!file) return
          e.preventDefault()
          composerSetAttachment.current?.(file)
        }}
      >
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="h-full overflow-y-auto px-4 py-4"
        >
          {loading && messages.length === 0 ? (
            <div className="py-10 text-center text-sm text-neutral-600">Loading…</div>
          ) : messages.length === 0 ? (
            <div className="py-10 text-center text-sm text-neutral-600">No messages</div>
          ) : (
            <>
              {hasMore && (
                <div className="mb-4 text-center">
                  <button
                    onClick={loadEarlier}
                    className="rounded-full border border-neutral-700 px-4 py-1 text-xs text-neutral-400 transition hover:bg-neutral-800"
                  >
                    Load earlier messages
                  </button>
                </div>
              )}
              <Timeline
                messages={messages}
                group={group}
                nameMap={nameMap}
                mentionIndex={mentionIndex}
                onOpenTask={onOpenTask}
                onTasksChanged={onTasksChanged}
                onOpenChat={onOpenChat}
                onReply={canSend ? (m) => { setEditingMsg(null); setReplyTo(m) } : undefined}
                onReact={canSend ? handleReact : undefined}
                onForward={setForwardMsg}
                onStar={handleStar}
                onEdit={canSend ? startEdit : undefined}
                onOpenImage={openLightboxFor}
                onJumpToMessage={jumpToMessage}
                selfDigits={selfDigits}
                highlightId={flashId || activeMatchId}
                unreadDivider={unreadDivider}
              />
            </>
          )}
        </div>
        {!atBottom && (
          <ScrollToBottomFab unread={unreadBelow} onClick={scrollToBottom} />
        )}
        {dragging && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-emerald-500/10 backdrop-blur-sm">
            <div className="rounded-2xl border-2 border-dashed border-emerald-400/70 bg-neutral-900/80 px-8 py-6 text-center text-sm text-emerald-100 shadow-lg">
              <div className="mb-2 text-3xl">📎</div>
              Drop to attach
            </div>
          </div>
        )}
      </div>

      {canSend && (
        <Composer
          jid={jid}
          group={group}
          initialText={composerDraft || initialDraft}
          replyTo={replyTo}
          editingMsg={editingMsg}
          nameMap={nameMap}
          onClearReply={() => setReplyTo(null)}
          onClearEdit={() => setEditingMsg(null)}
          onEdited={applyLocalEdit}
          onDraftConsumed={() => {
            if (composerDraft) setComposerDraft('')
            else onDraftConsumed?.()
          }}
          onSent={handleSent}
          setAttachmentRef={(setter) => {
            composerSetAttachment.current = setter
          }}
        />
      )}

      {showDrafts && (
        <DraftRepliesPopover
          jid={jid}
          onPick={(text) => {
            setComposerDraft(text)
            setShowDrafts(false)
          }}
          onClose={() => setShowDrafts(false)}
        />
      )}

      {showDashboard && (
        <DashboardModal
          kind={group ? 'group' : 'contact'}
          jid={jid}
          onOpenChat={(j) => {
            setShowDashboard(false)
            // Same chat: noop. Different chat would require an explorer-level open.
            if (j !== jid) onOpenChatTasks(j) // best-effort fallback
          }}
          onOpenTask={(id) => {
            setShowDashboard(false)
            onOpenTask(id)
          }}
          onOpenCircle={(id) => {
            setShowDashboard(false)
            onOpenCircle?.(id)
          }}
          onClose={() => setShowDashboard(false)}
        />
      )}

      {showHideDialog && (
        <HideChatDialog
          jid={jid}
          title={title}
          onDone={() => {
            setShowHideDialog(false)
            // Stay in the normal view after hiding — drop any unlock token so
            // the chat list refreshes locked (the hidden chat just disappears
            // from sight; the user is not flipped into "private mode").
            setUnlockToken(null)
            onTasksChanged()
            onCirclesChanged()
            window.dispatchEvent(new CustomEvent('wa.unlock-changed'))
          }}
          onClose={() => setShowHideDialog(false)}
        />
      )}

      {showHistory && (
        <ExtractionsModal
          title={title}
          fetchRuns={() => api.listExtractions(jid)}
          liveRunId={liveRunId}
          onClose={() => {
            setShowHistory(false)
            // After a fresh run, refresh tasks counter / show the chat tasks list.
            if (liveRunId) {
              onTasksChanged()
              onOpenChatTasks(jid)
            }
            setLiveRunId(null)
          }}
        />
      )}

      {lightboxIdx !== null && (
        <ImageLightbox
          images={lightboxImages}
          index={lightboxIdx}
          onIndex={setLightboxIdx}
          onClose={() => setLightboxIdx(null)}
        />
      )}

      {forwardMsg && (
        <ForwardPicker
          msg={forwardMsg}
          chats={chats}
          nameMap={nameMap}
          onClose={() => setForwardMsg(null)}
        />
      )}
    </div>
  )
}

// Composer is the message input at the bottom of a thread. Enter sends,
// Shift+Enter inserts a newline. dir="auto" keeps Arabic/English typing aligned.
// When a `replyTo` is set, the composer shows a quoted-message chip above the
// textarea and routes the send through api.reply instead of api.send — exactly
// like the official WA reply UX.
function Composer({
  jid,
  group,
  initialText = '',
  replyTo,
  editingMsg,
  nameMap,
  onClearReply,
  onClearEdit,
  onEdited,
  onDraftConsumed,
  onSent,
  setAttachmentRef,
}: {
  jid: string
  group: boolean
  initialText?: string
  replyTo?: Message | null
  /** When set, the composer is in edit mode for this (own) message: textarea
   *  pre-fills with the original body, Send routes through api.edit, and the
   *  attach button is disabled (WA's /edit only rewrites text). */
  editingMsg?: Message | null
  nameMap?: Map<string, string>
  onClearReply?: () => void
  /** Called when the user cancels edit mode (Esc, ✕ on the chip, or after
   *  the edit succeeds). */
  onClearEdit?: () => void
  /** Called after api.edit succeeds, so the thread can mutate the local
   *  message in-place (new text + is_edit=true) without a refetch. */
  onEdited?: (messageID: string, newText: string) => void
  onDraftConsumed?: () => void
  onSent: (m: Message) => void
  /** Optional callback the parent uses to push a File into the attachment
   *  slot from outside the composer — used by drag-and-drop on the thread. */
  setAttachmentRef?: (setter: ((f: File) => void) | null) => void
}) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  // Staged attachment (paperclip-picked, paste-pasted, or drop-dropped). It
  // lives entirely client-side until send() is called — at which point we
  // upload it, then either api.send or api.reply with the returned path.
  const [attachment, setAttachment] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  // Voice-recording state. Tapping the mic button asks for mic permission,
  // opens a MediaRecorder, and swaps the composer for a thin recording bar
  // (mic dot + mm:ss timer + ✕ cancel + ➤ send). On send we wrap the chunks
  // into a File and route through the same upload+send pipeline as any
  // staged attachment, so the bubble appears as an audio bubble locally and
  // the bridge ships it through whatsmeow's media path. WA's real PTT (the
  // "voice note" message_type with waveform metadata) needs a backend hop;
  // this v1 ships a standard audio attachment, which the recipient still
  // plays inline.
  const [isRecording, setIsRecording] = useState(false)
  const [recSeconds, setRecSeconds] = useState(0)
  const mediaRecorderRef = useRef<MediaRecorder | null>(null)
  const recChunksRef = useRef<Blob[]>([])
  const recTimerRef = useRef<number | null>(null)
  const recStreamRef = useRef<MediaStream | null>(null)
  // Discard-on-stop flag: cancel sets it true so the recorder's onstop
  // handler knows not to ship anything. Avoids a "send anyway" race when
  // stop() fires before we can wire a separate cancel path.
  const recDiscardRef = useRef(false)
  const taRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  // Emoji picker open/closed. Lives next to the attach button on the left
  // of the composer; click toggles, click-outside / Esc closes (handled
  // inside EmojiPicker so the textarea keeps its own keydown wiring clean).
  const [emojiOpen, setEmojiOpen] = useState(false)
  // Open mention-picker state: when the user is typing '@<query>' in the
  // textarea we open an autocomplete of group participants. `start` is the
  // caret position of the '@'; `query` is what's been typed after it.
  // Null when no picker is open.
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null)
  const [mentionIdx, setMentionIdx] = useState(0)
  // jidByDigits remembers which LID-digits we inserted as a mention token,
  // so on send we can collect MentionedJIDs from the final text without
  // re-querying anything. Survives across composer edits.
  const jidByDigits = useRef<Map<string, string>>(new Map())
  // Group participants, lazily loaded the first time we open the picker.
  const [participants, setParticipants] = useState<GroupParticipant[] | null>(null)
  useEffect(() => {
    if (!group) return
    let cancelled = false
    api.groupParticipants(jid).then((p) => { if (!cancelled) setParticipants(p) }).catch(() => {})
    return () => { cancelled = true }
  }, [group, jid])
  // Object URL of the attachment for the inline preview (image/video).
  // Revoked when the attachment changes / clears so we don't leak memory.
  const previewURL = useMemo(() => {
    if (!attachment) return ''
    if (!attachment.type.startsWith('image/') && !attachment.type.startsWith('video/')) return ''
    return URL.createObjectURL(attachment)
  }, [attachment])
  useEffect(() => {
    if (!previewURL) return
    return () => URL.revokeObjectURL(previewURL)
  }, [previewURL])

  // Focus + jump-to-end whenever a fresh reply target appears.
  useEffect(() => {
    if (!replyTo) return
    const el = taRef.current
    if (!el) return
    el.focus()
    const len = el.value.length
    el.setSelectionRange(len, len)
  }, [replyTo?.id])

  // Cancel reply with Escape, matching official WA.
  useEffect(() => {
    if (!replyTo) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClearReply?.()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [replyTo?.id, onClearReply])

  // Entering edit mode: copy the original body into the textarea, drop any
  // staged attachment (WA's edit can't carry media), focus + place caret at
  // the end so typing extends the existing text. Leaving edit mode (cleared
  // by parent) restores the empty composer.
  useEffect(() => {
    if (!editingMsg) {
      // Only clear the textarea on edit-exit if it still holds the original —
      // the user might have already moved on and typed something fresh.
      return
    }
    setAttachment(null)
    setText(editingMsg.content || '')
    requestAnimationFrame(() => {
      resize()
      const el = taRef.current
      if (!el) return
      el.focus()
      const len = el.value.length
      el.setSelectionRange(len, len)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editingMsg?.id])

  // Escape leaves edit mode without sending.
  useEffect(() => {
    if (!editingMsg) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setText('')
        onClearEdit?.()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [editingMsg?.id, onClearEdit])

  // Hand the parent a setter so drag-and-drop on the thread can stage an
  // attachment without colocating composer state up there.
  useEffect(() => {
    setAttachmentRef?.(setAttachment)
    return () => setAttachmentRef?.(null)
  }, [setAttachmentRef])

  function resize() {
    const el = taRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }

  // Apply a draft from a "Nudge" / "Reply in origin" action exactly once.
  // We don't overwrite user-typed text: only fill when the field is empty.
  useEffect(() => {
    if (!initialText) return
    if (text.trim() === '') {
      setText(initialText)
      requestAnimationFrame(resize)
      taRef.current?.focus()
      // place caret at the end so the user can keep typing
      requestAnimationFrame(() => {
        const el = taRef.current
        if (el) el.setSelectionRange(initialText.length, initialText.length)
      })
    }
    onDraftConsumed?.()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialText])

  // Per-chat draft persistence. Typed-but-not-sent text is saved on every
  // keystroke (see the textarea's onChange below) and restored when the
  // user comes back to the same chat — mirrors WA's own behavior where
  // switching threads never loses what you were typing. Keyed on jid so
  // each chat has its own buffer; cleared on send (see send() and
  // sendVoice()) so a sent message doesn't keep popping back.
  //
  // We deliberately save in onChange instead of a useEffect: it would
  // otherwise fire on every jid change, writing the OLD chat's text
  // under the NEW chat's key before the load completes.
  useEffect(() => {
    try {
      const saved = localStorage.getItem(draftKey(jid))
      setText(saved || '')
      requestAnimationFrame(resize)
    } catch {
      setText('')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jid])

  // --- voice recording ---

  // Ask the browser for the mic, start a MediaRecorder, and tick a 1s timer
  // so the UI can render mm:ss. We prefer webm/opus (Chrome) but fall back
  // to the browser default — on Safari that's mp4/aac, which the bridge
  // still treats as audio. Any error (permission denied, no device) is
  // surfaced through the same error banner the send() flow uses.
  async function startRecording() {
    if (isRecording) return
    setError('')
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      recStreamRef.current = stream
      const mime = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
        ? 'audio/webm;codecs=opus'
        : MediaRecorder.isTypeSupported('audio/webm')
        ? 'audio/webm'
        : ''
      const rec = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream)
      recChunksRef.current = []
      recDiscardRef.current = false
      rec.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) recChunksRef.current.push(e.data)
      }
      rec.onstop = () => {
        // Always stop the mic track so the browser drops the recording dot.
        recStreamRef.current?.getTracks().forEach((t) => t.stop())
        recStreamRef.current = null
        const shouldDiscard = recDiscardRef.current
        const chunks = recChunksRef.current
        recChunksRef.current = []
        if (!shouldDiscard && chunks.length > 0) {
          const type = rec.mimeType || 'audio/webm'
          const blob = new Blob(chunks, { type })
          sendVoice(blob, type).catch((e) => {
            setError(e instanceof Error ? e.message : 'Failed to send voice')
          })
        }
      }
      mediaRecorderRef.current = rec
      rec.start()
      setRecSeconds(0)
      setIsRecording(true)
      // 1-second ticker. Capped so a runaway recording can't blow up the
      // counter; 5 minutes is well past any reasonable voice note.
      recTimerRef.current = window.setInterval(() => {
        setRecSeconds((s) => (s >= 300 ? s : s + 1))
      }, 1000)
    } catch (e) {
      setError(
        e instanceof Error && e.name === 'NotAllowedError'
          ? 'Microphone permission denied'
          : e instanceof Error ? e.message : 'Could not start recording',
      )
    }
  }

  // Stop the recorder. `discard=true` throws the chunks away (cancel button);
  // false ships them through onstop → sendVoice. Either way we clean up the
  // timer + recording-mode UI immediately so the user gets snappy feedback,
  // even if the upload takes a moment.
  function stopRecording(discard: boolean) {
    const rec = mediaRecorderRef.current
    if (!rec) return
    recDiscardRef.current = discard
    if (recTimerRef.current) {
      window.clearInterval(recTimerRef.current)
      recTimerRef.current = null
    }
    setIsRecording(false)
    setRecSeconds(0)
    if (rec.state !== 'inactive') rec.stop()
    mediaRecorderRef.current = null
  }

  // Upload the recorded blob and ship it as a regular media send. Mirrors
  // the upload+send branch of send() but skipping the staged-attachment
  // state (which is async and would race with stopRecording). The local
  // echo uses media_type='audio' so the bubble renders with our existing
  // audio bubble (play/pause/progress), even before the SSE round-trip.
  async function sendVoice(blob: Blob, mime: string) {
    if (sending) return
    setSending(true)
    setError('')
    try {
      const ext = mime.includes('webm') ? 'webm' : mime.includes('mp4') ? 'm4a' : 'ogg'
      const file = new File([blob], `voice-${Date.now()}.${ext}`, { type: mime })
      setUploading(true)
      let mediaPath: string
      try {
        const up = await api.upload(file)
        mediaPath = up.path
      } finally {
        setUploading(false)
      }
      const res = replyTo
        ? await api.reply(jid, replyTo.id, '', { mediaPath })
        : await api.send(jid, '', { mediaPath })
      const echoed: Message = {
        id: res.message_id,
        chat_jid: jid,
        sender: '',
        sender_name: '',
        push_name: '',
        content: '',
        timestamp: res.timestamp,
        is_from_me: true,
        is_group: group,
        message_type: 'media',
        media_type: 'audio',
        media_filename: file.name,
        media_size: file.size,
        media_mime: mime,
      }
      if (replyTo) {
        echoed.reply_to_id = replyTo.id
        echoed.reply_to_sender = replyTo.sender
        echoed.reply_to_content =
          replyTo.content || replyTo.media_caption || mediaWord(replyTo.media_type)
      }
      onSent(echoed)
      onClearReply?.()
    } finally {
      setSending(false)
    }
  }

  // Clean up any in-flight recording when the composer unmounts (e.g. user
  // switches chats mid-record). Otherwise the mic stays open in the
  // browser's tab bar.
  useEffect(() => {
    return () => {
      if (recTimerRef.current) window.clearInterval(recTimerRef.current)
      recStreamRef.current?.getTracks().forEach((t) => t.stop())
      const rec = mediaRecorderRef.current
      if (rec && rec.state !== 'inactive') {
        recDiscardRef.current = true
        try { rec.stop() } catch {}
      }
    }
  }, [])

  async function send() {
    const body = text.trim()
    // Edit-mode path: rewrite the existing message's text and bail before any
    // upload / send routing. Empty edits are blocked (WA does the same — the
    // /edit endpoint also rejects empty new_text).
    if (editingMsg) {
      if (!body || body === editingMsg.content) {
        // No-op: nothing to save / nothing changed. Just leave edit mode.
        onClearEdit?.()
        setText('')
        return
      }
      if (sending) return
      setSending(true)
      setError('')
      try {
        await api.edit(jid, editingMsg.id, body)
        onEdited?.(editingMsg.id, body)
        onClearEdit?.()
        setText('')
        requestAnimationFrame(resize)
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Failed to edit')
      } finally {
        setSending(false)
      }
      return
    }
    // Allow caption-less media sends — WA does too.
    if (!body && !attachment) return
    if (sending) return
    setSending(true)
    setError('')
    try {
      // Upload first if there's an attachment, so the bridge has a real
      // server-side path to feed to whatsmeow's media upload.
      let mediaPath: string | undefined
      if (attachment) {
        setUploading(true)
        try {
          const up = await api.upload(attachment)
          mediaPath = up.path
        } finally {
          setUploading(false)
        }
      }
      // Routing matrix:
      //   reply + media  → /reply with media_path (already supported)
      //   reply + text   → /reply
      //   media          → /send with media_path
      //   text           → /send
      const mentionedJIDs = collectMentionedJIDs()
      const opts =
        mediaPath || mentionedJIDs.length > 0
          ? { mediaPath, mentionedJIDs: mentionedJIDs.length > 0 ? mentionedJIDs : undefined }
          : undefined
      const res = replyTo
        ? await api.reply(jid, replyTo.id, body, opts)
        : await api.send(jid, body, opts)
      // Echo locally so the bubble lands instantly. For media we don't have
      // the server's permanent path yet, but the caption + a placeholder
      // media_type is enough to render a "Photo" / "Video" / "Document" chip
      // until the SSE round-trip overwrites it with the real download.
      const mediaKind = attachment ? guessMediaKind(attachment) : ''
      const echoed: Message = {
        id: res.message_id,
        chat_jid: jid,
        sender: '',
        sender_name: '',
        push_name: '',
        content: body,
        timestamp: res.timestamp,
        is_from_me: true,
        is_group: group,
        message_type: mediaKind ? 'media' : 'text',
      }
      if (mediaKind) {
        echoed.media_type = mediaKind
        echoed.media_caption = body
        echoed.media_filename = attachment?.name
        echoed.media_size = attachment?.size
      }
      if (replyTo) {
        echoed.reply_to_id = replyTo.id
        echoed.reply_to_sender = replyTo.sender
        echoed.reply_to_content =
          replyTo.content || replyTo.media_caption || mediaWord(replyTo.media_type)
      }
      onSent(echoed)
      onClearReply?.()
      setText('')
      setAttachment(null)
      // The text-change effect already removes empty drafts, but be explicit
      // here so the key disappears synchronously with the successful send —
      // no chance of a stale draft surviving a fast chat-switch. Notify the
      // chat list so its "Draft: …" pill clears in the same tick.
      try {
        localStorage.removeItem(draftKey(jid))
        window.dispatchEvent(new CustomEvent('wa.draft-changed'))
      } catch {}
      requestAnimationFrame(resize)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to send')
    } finally {
      setSending(false)
    }
  }

  // updateMentionContext re-scans the textarea around the caret to find an
  // open '@<query>' token and open/close the picker accordingly. Triggered
  // on input + on selection change. The token is the contiguous word the
  // caret sits in, starting with '@' followed by letters/digits (no spaces,
  // no newlines, no further '@').
  function updateMentionContext(value: string, caret: number) {
    if (!group) {
      if (mention) setMention(null)
      return
    }
    // Walk backward from caret until we hit whitespace or '@'.
    let i = caret
    while (i > 0) {
      const ch = value[i - 1]
      if (ch === '@') {
        const start = i - 1
        // The char before '@' must be start-of-text or whitespace — otherwise
        // we're looking at an email-like string, not a mention.
        if (start === 0 || /\s/.test(value[start - 1])) {
          const query = value.slice(start + 1, caret)
          // Reject if the query itself contains weird chars; bail otherwise.
          if (/^[\p{L}\p{N}._-]*$/u.test(query)) {
            setMention({ start, query })
            setMentionIdx(0)
            return
          }
        }
        break
      }
      if (/\s/.test(ch)) break
      i--
    }
    if (mention) setMention(null)
  }

  // Filter participants by the open query. Match against display name (any
  // word starting with the query) AND phone digits / LID digits, so typing
  // either a name or a prefix of the number works.
  const filteredParticipants = useMemo<GroupParticipant[]>(() => {
    if (!mention || !participants) return []
    const q = mention.query.toLowerCase().trim()
    const all = participants.slice()
    if (!q) return all.slice(0, 8)
    const out: GroupParticipant[] = []
    for (const p of all) {
      const name = (p.display_name || '').toLowerCase()
      const phone = (p.phone || '').toLowerCase()
      const lid = (p.lid || '').toLowerCase()
      if (
        name.includes(q) ||
        phone.startsWith(q) ||
        lid.startsWith(q) ||
        nameMap?.get(p.jid)?.toLowerCase().includes(q)
      ) {
        out.push(p)
        if (out.length >= 8) break
      }
    }
    return out
  }, [mention?.query, participants, nameMap])

  // pickMention replaces the '@<query>' token in the textarea with
  // '@<LID-digits> ' and remembers the JID so we can include it in
  // mentioned_jids on send. LID-digits is the form WA's wire protocol
  // expects in mention tokens, and matches what /messages stores.
  function pickMention(p: GroupParticipant) {
    if (!mention) return
    // Prefer LID digits (the form WA stores mentions as), fall back to phone
    // digits when the participant has no LID resolved yet.
    const lidDigits = (p.lid || '').split('@')[0].split(':')[0]
    const phoneDigits = (p.phone || p.jid || '').split('@')[0].split(':')[0]
    const digits = lidDigits || phoneDigits
    if (!digits) return
    // Remember the digits → full JID mapping so send() can collect the
    // MentionedJID list later. The bridge stores `lid` as bare digits but
    // `jid` always carries the full suffix (@lid or @s.whatsapp.net),
    // which is what whatsmeow needs to actually ping someone.
    const fullJID = lidDigits ? `${lidDigits}@lid` : p.jid
    jidByDigits.current.set(digits, fullJID)
    // Splice the token into the textarea: drop '@<query>', insert
    // '@<digits> ' (with a trailing space — feels right after picking).
    const before = text.slice(0, mention.start)
    const after = text.slice(mention.start + 1 + mention.query.length)
    const insert = '@' + digits + ' '
    const next = before + insert + after
    setText(next)
    setMention(null)
    setMentionIdx(0)
    // Restore focus + place caret right after the inserted token.
    requestAnimationFrame(() => {
      const el = taRef.current
      if (!el) return
      el.focus()
      const pos = before.length + insert.length
      el.setSelectionRange(pos, pos)
      resize()
    })
  }

  // Insert one emoji at the textarea's current caret position, then keep
  // the caret right after it. Mirrors how WA's mobile composer behaves —
  // picker stays open so you can rattle off several in a row, only Esc /
  // click-outside actually closes it. Also persists the draft (same path
  // the textarea's onChange would have taken if the user had typed it).
  function insertEmoji(emoji: string) {
    const el = taRef.current
    const caret = el?.selectionStart ?? text.length
    const before = text.slice(0, caret)
    const after = text.slice(caret)
    const next = before + emoji + after
    setText(next)
    if (!editingMsg) {
      try {
        if (next.trim()) localStorage.setItem(draftKey(jid), next)
        else localStorage.removeItem(draftKey(jid))
        window.dispatchEvent(new CustomEvent('wa.draft-changed'))
      } catch {}
    }
    requestAnimationFrame(() => {
      const el2 = taRef.current
      if (!el2) return
      el2.focus()
      const pos = before.length + emoji.length
      el2.setSelectionRange(pos, pos)
      resize()
    })
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // While the @-picker is open, arrow keys / Enter / Tab / Esc target the
    // picker — never the textarea. Otherwise Enter sends, Shift+Enter is a
    // newline (existing behavior).
    if (mention && filteredParticipants.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setMentionIdx((i) => (i + 1) % filteredParticipants.length)
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setMentionIdx((i) => (i - 1 + filteredParticipants.length) % filteredParticipants.length)
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        pickMention(filteredParticipants[mentionIdx])
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setMention(null)
        return
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  // Paste-handler turns ⌘V of an image (or any file) on the textarea into a
  // staged attachment — same shortcut WA users expect.
  function onPaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    const f = Array.from(e.clipboardData.files).find((f) => f.size > 0)
    if (f) {
      e.preventDefault()
      setAttachment(f)
    }
  }

  // collectMentionedJIDs walks the typed text, picks every '@<digits>' token
  // whose digits we previously inserted via pickMention, and returns the
  // matching JIDs. Done at send time (not before) so deleted mentions don't
  // get pinged — if the user backspaces over a token it shouldn't ping.
  function collectMentionedJIDs(): string[] {
    const seen = new Set<string>()
    const re = /@(\d{7,16})\b/g
    let m: RegExpExecArray | null
    while ((m = re.exec(text)) !== null) {
      const jid = jidByDigits.current.get(m[1])
      if (jid) seen.add(jid)
    }
    return Array.from(seen)
  }

  // Send is allowed when there's typed text or a staged attachment, except
  // in edit mode where only text counts (attachments are disabled anyway).
  const canSendNow = editingMsg
    ? text.trim().length > 0 && !sending
    : (text.trim().length > 0 || attachment !== null) && !sending

  // Show the mic button instead of send when the user has nothing typed and
  // no attachment staged — matches WA, where the right-side icon morphs
  // between paper-plane and microphone depending on intent.
  const showMicInsteadOfSend = !editingMsg && text.trim().length === 0 && !attachment

  return (
    <div className="border-t border-neutral-800 px-4 py-3">
      {error && <div className="mb-1 text-xs text-red-400">{error}</div>}
      {isRecording ? (
        <RecordingBar
          seconds={recSeconds}
          uploading={uploading || sending}
          onCancel={() => stopRecording(true)}
          onSend={() => stopRecording(false)}
        />
      ) : (
        <>
          {editingMsg ? (
            <EditingChip onClear={() => { setText(''); onClearEdit?.() }} />
          ) : replyTo ? (
            <ReplyQuote
              msg={replyTo}
              nameMap={nameMap || new Map()}
              onClear={() => onClearReply?.()}
            />
          ) : null}
          {attachment && !editingMsg && (
            <AttachmentPreview
              file={attachment}
              previewURL={previewURL}
              onClear={() => setAttachment(null)}
            />
          )}
          <input
            ref={fileRef}
            type="file"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) setAttachment(f)
              // Reset so picking the same file again still fires onChange.
              e.target.value = ''
            }}
          />
          <div className="flex items-end gap-2">
            <button
              onClick={() => fileRef.current?.click()}
              disabled={!!editingMsg}
              title={editingMsg ? 'Attachments are disabled while editing' : 'Attach a file'}
              aria-label="Attach a file"
              className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
            >
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21.44 11.05 12.25 20.24a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66L9.41 17.41a2 2 0 0 1-2.83-2.83l8.49-8.49" />
              </svg>
            </button>
            <div className="relative shrink-0">
              <button
                onClick={() => setEmojiOpen((v) => !v)}
                title="Insert emoji"
                aria-label="Insert emoji"
                aria-expanded={emojiOpen}
                className={
                  'flex h-10 w-10 items-center justify-center rounded-full transition ' +
                  (emojiOpen
                    ? 'bg-neutral-800 text-amber-300'
                    : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200')
                }
              >
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="9" />
                  <path d="M8 14s1.5 2 4 2 4-2 4-2" />
                  <circle cx="9" cy="10" r="0.6" fill="currentColor" />
                  <circle cx="15" cy="10" r="0.6" fill="currentColor" />
                </svg>
              </button>
              {emojiOpen && (
                <EmojiPicker
                  onPick={insertEmoji}
                  onClose={() => setEmojiOpen(false)}
                />
              )}
            </div>
            <div className="relative flex-1">
              {mention && filteredParticipants.length > 0 && (
                <MentionPicker
                  participants={filteredParticipants}
                  nameMap={nameMap}
                  highlighted={mentionIdx}
                  onPick={pickMention}
                  onHover={setMentionIdx}
                />
              )}
              <textarea
                ref={taRef}
                dir="auto"
                rows={1}
                value={text}
                onChange={(e) => {
                  const v = e.target.value
                  setText(v)
                  resize()
                  updateMentionContext(v, e.target.selectionStart)
                  // Persist per-chat draft on every keystroke (skipped while
                  // editing — see the draft persistence note above). The
                  // custom event lets the chat list's "Draft: …" indicator
                  // refresh live; localStorage's `storage` event only fires
                  // in *other* tabs, not the one that did the write.
                  if (!editingMsg) {
                    try {
                      if (v.trim()) localStorage.setItem(draftKey(jid), v)
                      else localStorage.removeItem(draftKey(jid))
                      window.dispatchEvent(new CustomEvent('wa.draft-changed'))
                    } catch {}
                  }
                }}
                onKeyDown={onKeyDown}
                onPaste={onPaste}
                onSelect={(e) => updateMentionContext(text, (e.target as HTMLTextAreaElement).selectionStart)}
                placeholder={
                  editingMsg ? 'Edit message…' : attachment ? 'Add a caption…' : 'Type a message'
                }
                className="max-h-40 min-h-[2.5rem] w-full resize-none rounded-2xl border border-neutral-800 bg-neutral-900 px-3 py-2 text-sm outline-none placeholder:text-neutral-600 focus:border-neutral-600"
              />
            </div>
            {showMicInsteadOfSend ? (
              <button
                onClick={startRecording}
                title="Record voice message"
                aria-label="Record voice message"
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
              >
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="9" y="3" width="6" height="12" rx="3" />
                  <path d="M5 11a7 7 0 0 0 14 0" />
                  <path d="M12 18v3" />
                </svg>
              </button>
            ) : (
              <button
                onClick={send}
                disabled={!canSendNow}
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
                title={editingMsg ? 'Save edit' : uploading ? 'Uploading…' : 'Send'}
              >
                {sending ? '…' : editingMsg ? '✓' : '➤'}
              </button>
            )}
          </div>
        </>
      )}
    </div>
  )
}

// AttachmentPreview shows the staged file above the textarea so the user can
// confirm what they're about to send. Images and videos render as a small
// thumbnail; everything else shows a paperclip + filename + size, the same
// shorthand WA uses in its "send file" sheet.
function AttachmentPreview({
  file,
  previewURL,
  onClear,
}: {
  file: File
  previewURL: string
  onClear: () => void
}) {
  const sizeStr =
    file.size < 1024
      ? `${file.size} B`
      : file.size < 1024 * 1024
        ? `${(file.size / 1024).toFixed(0)} KB`
        : `${(file.size / (1024 * 1024)).toFixed(1)} MB`
  const isImage = file.type.startsWith('image/')
  const isVideo = file.type.startsWith('video/')
  return (
    <div className="mb-2 flex items-center gap-3 rounded-lg bg-neutral-900 p-2 text-xs">
      {isImage && previewURL ? (
        <img src={previewURL} alt="" className="h-14 w-14 rounded object-cover" />
      ) : isVideo && previewURL ? (
        <video src={previewURL} className="h-14 w-14 rounded object-cover" muted />
      ) : (
        <div className="flex h-14 w-14 items-center justify-center rounded bg-neutral-800 text-2xl">
          📄
        </div>
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-neutral-200">{file.name || 'Attachment'}</div>
        <div className="text-neutral-500">{sizeStr}</div>
      </div>
      <button
        onClick={onClear}
        title="Remove attachment"
        aria-label="Remove attachment"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
  )
}

// useDmPresence subscribes the bridge to presence updates for `jid` once,
// then polls the cached entry every few seconds while the chat is open.
// whatsmeow only pushes presence beacons after a subscribe call, so the
// first poll usually returns nothing — that's fine; the header just stays
// on the fallback (phone number) until the first update lands.
//
// Pass `null` to disable (groups, status, newsletters).
function useDmPresence(jid: string | null): PresenceEntry | null {
  const [entry, setEntry] = useState<PresenceEntry | null>(null)
  useEffect(() => {
    if (!jid) {
      setEntry(null)
      return
    }
    let cancelled = false
    // Subscribe is fire-and-forget — failures (e.g. peer blocks presence)
    // are normal and shouldn't pollute the UI with errors.
    api.presenceSubscribe(jid).catch(() => {})
    async function tick() {
      const p = await api.presenceGet(jid as string).catch(() => null)
      if (!cancelled) setEntry(p)
    }
    void tick()
    const h = setInterval(tick, 3000)
    return () => {
      cancelled = true
      clearInterval(h)
    }
  }, [jid])
  return entry
}

// formatPresence turns a PresenceEntry into the short string WA shows under
// the chat name: 'typing…', 'online', 'last seen today at 14:32', etc.
// Returns '' when there is nothing meaningful to show, so the caller can
// fall back to the phone number.
function formatPresence(p: PresenceEntry): string {
  const now = Math.floor(Date.now() / 1000)
  const age = now - (p.updated_at || 0)
  // Active typing — but only while the beacon is recent. The peer stops
  // sending 'composing' once they pause, so an old composing entry isn't
  // really 'still typing'. WA itself ages these out in ~10s.
  if (p.status === 'composing' && age < 10) return 'typing…'
  // 'available' (online) is also a beacon — peers send periodic refreshes;
  // assume offline after ~90s of silence.
  if (p.status === 'available' && age < 90) return 'online'
  // Otherwise show last-seen if we have it. last_seen=0 means the peer hid
  // it via WA privacy — render nothing rather than a misleading 'never'.
  if (p.status === 'unavailable' && p.last_seen && p.last_seen > 0) {
    return 'last seen ' + formatLastSeen(p.last_seen)
  }
  return ''
}

// useGroupTyping polls /chats/{jid}/typing every 3 s for groups, returning
// the set of sender JIDs currently composing. The bridge's in-memory cache
// auto-expires entries after ~10s, so a stale list never lingers.
//
// Pass `null` to disable (used for DMs / status / newsletters).
function useGroupTyping(jid: string | null): string[] {
  const [typers, setTypers] = useState<string[]>([])
  useEffect(() => {
    if (!jid) {
      setTypers([])
      return
    }
    let cancelled = false
    async function tick() {
      const list = await api.chatTyping(jid as string).catch(() => [] as string[])
      if (!cancelled) setTypers(list)
    }
    void tick()
    const h = setInterval(tick, 3000)
    return () => {
      cancelled = true
      clearInterval(h)
    }
  }, [jid])
  return typers
}

// formatGroupTyping turns the typer set into the WA-style header string,
// resolved through nameMap so we show display names rather than raw JIDs:
//
//   1 typer  → "Sarah is typing…"
//   2 typers → "Sarah and Aymen are typing…"
//   3+       → "Several people are typing…"  (matches official WA cap)
//
// Returns '' when nobody is typing, so the caller can fall back to "Group".
function formatGroupTyping(jids: string[], nameMap: Map<string, string>): string {
  if (jids.length === 0) return ''
  // Resolve to first names (split on whitespace so "Mohammed Shurrab" → "Mohammed")
  const names = jids.map((j) => {
    const full = nameMap.get(j) || ''
    if (full) return full.split(/\s+/)[0]
    // Fall back to the bare phone digits, prefixed, so the header isn't blank.
    return '+' + (j.split('@')[0] || '').split(':')[0]
  })
  if (names.length === 1) return `${names[0]} is typing…`
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing…`
  return 'Several people are typing…'
}

// formatLastSeen mirrors the WA format: 'today at 14:32', 'yesterday at
// 18:05', otherwise 'May 19 at 11:40' / 'Mar 3, 2025 at 09:15' for older.
function formatLastSeen(ts: number): string {
  const d = new Date(ts * 1000)
  const today = new Date()
  const yesterday = new Date()
  yesterday.setDate(today.getDate() - 1)
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  const hh = d.getHours().toString().padStart(2, '0')
  const mm = d.getMinutes().toString().padStart(2, '0')
  const time = `${hh}:${mm}`
  if (sameDay(d, today)) return `today at ${time}`
  if (sameDay(d, yesterday)) return `yesterday at ${time}`
  const datePart = d.toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric',
  })
  return `${datePart} at ${time}`
}

// draftKey is the localStorage namespace for the Composer's per-chat draft
// persistence. Kept as a tiny helper so the prefix lives in one place and
// future cleanup (a "clear all drafts" action, a version bump, etc.) only
// has to touch this line.
function draftKey(jid: string): string {
  return `wa.draft.${jid}`
}

// guessMediaKind maps a browser File's MIME type to the same media_type the
// bridge stores — used to render an accurate local echo bubble before the
// SSE re-fetch lands.
function guessMediaKind(f: File): string {
  if (f.type.startsWith('image/')) return 'image'
  if (f.type.startsWith('video/')) return 'video'
  if (f.type.startsWith('audio/')) return 'audio'
  return 'document'
}

// ReplyQuote is the small chip the Composer renders above its textarea while a
// reply target is staged — mirrors the official WA "you're replying to X"
// preview: colored vertical stripe, sender name in their per-sender color, a
// single-line snippet of the quoted body, and an × to cancel.
// MentionPicker is the small autocomplete that floats above the textarea
// while the user is typing '@<query>' inside a group. Up/Down highlight,
// Enter/Tab pick, Esc cancels — all handled by the composer's onKeyDown.
// We just render the list and call onHover/onPick for mouse interaction.
function MentionPicker({
  participants,
  nameMap,
  highlighted,
  onPick,
  onHover,
}: {
  participants: GroupParticipant[]
  nameMap?: Map<string, string>
  highlighted: number
  onPick: (p: GroupParticipant) => void
  onHover: (i: number) => void
}) {
  return (
    <div className="absolute bottom-full left-0 right-0 z-30 mb-1 max-h-60 overflow-y-auto rounded-xl bg-neutral-900 shadow-lg ring-1 ring-neutral-800">
      {participants.map((p, i) => {
        // Label precedence matches the rest of the UI (chatTitle / senderTitle):
        // GroupParticipant.display_name first, then the contact name resolved
        // through nameMap (which buildNameMap stitches together from contacts
        // and groups), then the raw '+<phone>' fallback so we never show
        // just the JID.
        const name =
          p.display_name ||
          nameMap?.get(p.jid) ||
          ('+' + (p.phone || p.jid.split('@')[0] || ''))
        const phone = p.phone ? '+' + p.phone : ''
        const initial = (name.replace(/^\+/, '').trim()[0] || '?').toUpperCase()
        const active = i === highlighted
        return (
          <button
            key={p.jid}
            onMouseDown={(e) => { e.preventDefault(); onPick(p) }}
            onMouseEnter={() => onHover(i)}
            className={
              'flex w-full items-center gap-2 px-3 py-2 text-left transition ' +
              (active ? 'bg-emerald-500/15' : 'hover:bg-neutral-800')
            }
          >
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-700 text-xs font-semibold text-neutral-200">
              {initial}
            </div>
            <div className="min-w-0 flex-1">
              <div dir="auto" className="truncate text-sm text-neutral-100">
                {name}
              </div>
              {phone && phone !== name && (
                <div className="truncate text-[11px] text-neutral-500">{phone}</div>
              )}
            </div>
            {(p.is_admin || p.is_super_admin) && (
              <span className="shrink-0 text-[10px] uppercase tracking-wider text-emerald-300">
                {p.is_super_admin ? 'owner' : 'admin'}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

// SearchBar is the slim find-in-chat strip that drops in between the chat
// header and the message list when the user clicks the magnifying glass.
// Mirrors the keyboard ergonomics of WA's own search bar: typing filters
// matches, Enter or ↓ advances, Shift+Enter or ↑ steps back, Esc closes.
// The "X / Y" counter goes faint while there are no matches so the user
// can see at a glance that they've typed past anything in the loaded
// window (use "Load earlier messages" to widen the haystack).
function SearchBar({
  query,
  onQueryChange,
  matchCount,
  activeIdx,
  onPrev,
  onNext,
  onClose,
}: {
  query: string
  onQueryChange: (q: string) => void
  matchCount: number
  activeIdx: number
  onPrev: () => void
  onNext: () => void
  onClose: () => void
}) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    ref.current?.focus()
  }, [])
  const hasHits = matchCount > 0
  const positionLabel = hasHits ? `${activeIdx + 1} / ${matchCount}` : query ? '0 / 0' : ''
  return (
    <div className="flex items-center gap-2 border-b border-neutral-800 bg-neutral-950 px-4 py-2">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-neutral-500" aria-hidden="true">
        <circle cx="11" cy="11" r="7" />
        <path d="m21 21-4.3-4.3" />
      </svg>
      <input
        ref={ref}
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') { e.preventDefault(); onClose(); return }
          if (e.key === 'Enter') {
            e.preventDefault()
            e.shiftKey ? onPrev() : onNext()
            return
          }
          if (e.key === 'ArrowDown') { e.preventDefault(); onNext(); return }
          if (e.key === 'ArrowUp')   { e.preventDefault(); onPrev(); return }
        }}
        placeholder="Search in chat…"
        className="flex-1 bg-transparent text-sm outline-none placeholder:text-neutral-600"
      />
      {positionLabel && (
        <span
          className={
            'shrink-0 text-[11px] tabular-nums ' +
            (hasHits ? 'text-neutral-400' : 'text-red-400/80')
          }
        >
          {positionLabel}
        </span>
      )}
      <button
        onClick={onPrev}
        disabled={!hasHits}
        title="Previous match (Shift+Enter)"
        aria-label="Previous match"
        className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200 disabled:cursor-not-allowed disabled:opacity-40"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="18 15 12 9 6 15" />
        </svg>
      </button>
      <button
        onClick={onNext}
        disabled={!hasHits}
        title="Next match (Enter)"
        aria-label="Next match"
        className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200 disabled:cursor-not-allowed disabled:opacity-40"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>
      <button
        onClick={onClose}
        title="Close search (Esc)"
        aria-label="Close search"
        className="flex h-7 w-7 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
  )
}

// RecordingBar takes over the composer footer while a voice note is being
// recorded. Mirrors WhatsApp's recording UX in a desktop-friendly form: a
// pulsing red dot, a mm:ss timer that ticks every second, a ✕ to discard
// and a green ➤ to stop + send. Press Esc anywhere to discard, matching
// the rest of the composer's cancel-with-escape muscle memory.
function RecordingBar({
  seconds,
  uploading,
  onCancel,
  onSend,
}: {
  seconds: number
  uploading: boolean
  onCancel: () => void
  onSend: () => void
}) {
  // Esc to discard — installed only while the bar is mounted, so it doesn't
  // collide with the other Esc handlers (reply, edit) when they're inactive.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onCancel()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel])
  const mm = Math.floor(seconds / 60).toString().padStart(1, '0')
  const ss = (seconds % 60).toString().padStart(2, '0')
  return (
    <div className="flex items-center gap-3 rounded-2xl bg-neutral-900 px-3 py-2">
      <button
        onClick={onCancel}
        title="Discard (Esc)"
        aria-label="Discard recording"
        className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-neutral-400 transition hover:bg-neutral-800 hover:text-red-300"
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="3 6 5 6 21 6" />
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
        </svg>
      </button>
      <div className="flex flex-1 items-center gap-2 text-sm text-neutral-200">
        {/* Pulsing red dot — same affordance WA uses to say "live mic". */}
        <span className="relative inline-flex h-3 w-3">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-500 opacity-60" />
          <span className="relative inline-flex h-3 w-3 rounded-full bg-red-500" />
        </span>
        <span className="font-mono tabular-nums">
          {mm}:{ss}
        </span>
        <span className="text-xs text-neutral-500">
          {uploading ? 'Sending…' : 'Recording'}
        </span>
      </div>
      <button
        onClick={onSend}
        disabled={uploading}
        title="Send voice message"
        aria-label="Send voice message"
        className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
      >
        {uploading ? '…' : '➤'}
      </button>
    </div>
  )
}

// ScrollToBottomFab is the small floating ↓ button WhatsApp pops at the
// bottom-right of a chat thread once you scroll up out of the live view.
// Tap it to smooth-scroll back to the newest message. When new incoming
// messages have arrived while you were reading history we stack a small
// emerald badge on top with the count — same shorthand as official WA's
// "↓ 3" pill. 99+ caps the badge so it never breaks the circle.
function ScrollToBottomFab({
  unread,
  onClick,
}: {
  unread: number
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      title="Scroll to bottom"
      aria-label="Scroll to bottom"
      className="absolute bottom-4 right-4 z-20 flex h-10 w-10 items-center justify-center rounded-full bg-neutral-800 text-neutral-200 shadow-lg ring-1 ring-neutral-700 transition hover:bg-neutral-700"
    >
      <svg
        viewBox="0 0 24 24"
        width="18"
        height="18"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <polyline points="6 9 12 15 18 9" />
      </svg>
      {unread > 0 && (
        <span className="absolute -right-1 -top-1 flex h-5 min-w-[20px] items-center justify-center rounded-full bg-emerald-500 px-1 text-[10px] font-semibold tabular-nums text-neutral-950">
          {unread > 99 ? '99+' : unread}
        </span>
      )}
    </button>
  )
}

// EditingChip is the slim banner the Composer shows above the textarea while
// the user is editing one of their own messages. Sits in the same slot the
// ReplyQuote uses for a fresh reply — at most one of the two is visible at a
// time, mirroring official WA's mutually-exclusive edit / reply UX.
function EditingChip({ onClear }: { onClear: () => void }) {
  return (
    <div
      className="mb-2 flex items-center gap-2 rounded-lg bg-neutral-900 py-1.5 pr-2 text-xs"
      style={{ borderInlineStart: `3px solid #06cf9c`, paddingInlineStart: '10px' }}
    >
      <svg
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="#06cf9c"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 20h9" />
        <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" />
      </svg>
      <div className="flex-1">
        <div className="text-[11px] font-semibold text-emerald-300">Editing message</div>
        <div className="text-neutral-500">Press Esc to cancel · Enter to save</div>
      </div>
      <button
        onClick={onClear}
        title="Cancel edit (Esc)"
        aria-label="Cancel edit"
        className="-mr-1 mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
  )
}

function ReplyQuote({
  msg,
  nameMap,
  onClear,
}: {
  msg: Message
  nameMap: Map<string, string>
  onClear: () => void
}) {
  const youSent = msg.is_from_me
  const senderLabel = youSent
    ? 'You'
    : senderTitle(msg.sender, msg.sender_name, msg.push_name, nameMap)
  const color = youSent ? '#06cf9c' : senderColor(msg.sender)
  const snippet =
    msg.content || msg.media_caption || mediaWord(msg.media_type) || 'Message'
  return (
    <div
      className="mb-2 flex items-start gap-2 rounded-lg bg-neutral-900 py-1.5 pr-2 text-xs"
      style={{ borderInlineStart: `3px solid ${color}`, paddingInlineStart: '10px' }}
    >
      <div className="min-w-0 flex-1">
        <div className="text-[11px] font-semibold" style={{ color }}>
          {senderLabel}
        </div>
        <div dir="auto" className="line-clamp-1 text-neutral-300">
          {snippet}
        </div>
      </div>
      <button
        onClick={onClear}
        title="Cancel reply (Esc)"
        aria-label="Cancel reply"
        className="-mr-1 mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
  )
}

// mediaWord turns a media type into a one-word stand-in for the chip / echo
// when the original message has no text body — same shorthand the chat-list
// preview already uses.
function mediaWord(t?: string): string {
  switch (t) {
    case 'image': return '📷 Photo'
    case 'video': return '🎥 Video'
    case 'voice_note': return '🎤 Voice message'
    case 'audio': return '🎵 Audio'
    case 'document': return '📄 Document'
    case 'sticker': return '🌟 Sticker'
    default: return ''
  }
}

// Timeline renders bubbles with day separators between different dates.
function Timeline({
  messages,
  group,
  nameMap,
  mentionIndex,
  onOpenTask,
  onTasksChanged,
  onOpenChat,
  onReply,
  onReact,
  onForward,
  onStar,
  onEdit,
  onOpenImage,
  onJumpToMessage,
  selfDigits,
  highlightId,
  unreadDivider,
}: {
  messages: Message[]
  group: boolean
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChat?: (jid: string) => void
  onReply?: (msg: Message) => void
  onReact?: (msg: Message, emoji: string) => void
  onForward?: (msg: Message) => void
  onStar?: (msg: Message, starred: boolean) => void
  onEdit?: (msg: Message) => void
  onOpenImage?: (msg: Message) => void
  /** Click handler for the quoted-reply preview chip — jumps to + flashes
   *  the original message. No-ops silently if the target isn't loaded. */
  onJumpToMessage?: (id: string) => void
  selfDigits?: Set<string>
  /** Message ID of the current search match — gets an emerald ring around
   *  its bubble + is the scrollIntoView target driven from MessageThread. */
  highlightId?: string
  /** When set, render a "N unread messages" divider just before the row
   *  with id = beforeId. Snapshotted at chat-open so the line stays put. */
  unreadDivider?: { beforeId: string; count: number } | null
}) {
  // Same-sender bursts cluster together: a new day, a new sender, or a >60s
  // gap from the previous message ends one cluster and starts another. WA does
  // exactly this — sender label only on the first bubble of the cluster, and
  // a tighter vertical gap between bubbles inside it.
  const CLUSTER_GAP_S = 60
  let lastDay = ''
  let lastKey = ''
  let lastTs = 0
  return (
    <div className="flex flex-col">
      {messages.map((m) => {
        const day = dayLabel(m.timestamp)
        const sep = day !== lastDay
        lastDay = day
        const senderKey = m.is_from_me ? '__me__' : m.sender
        const firstInGroup =
          sep || senderKey !== lastKey || m.timestamp - lastTs > CLUSTER_GAP_S
        lastKey = senderKey
        lastTs = m.timestamp
        return (
          <div key={m.id + m.timestamp} data-msg-id={m.id}>
            {sep && (
              <div className="my-3 flex justify-center">
                <span className="rounded-full bg-neutral-800 px-3 py-1 text-[11px] text-neutral-400">
                  {day}
                </span>
              </div>
            )}
            {unreadDivider && unreadDivider.beforeId === m.id && (
              // Horizontal "N unread messages" line, drawn just before the
              // first unread incoming message. Emerald so it pops against
              // the neutral thread without competing with day separators.
              <div className="my-3 flex items-center gap-3 text-[11px] font-medium text-emerald-300/80">
                <div className="h-px flex-1 bg-emerald-500/30" />
                <span className="uppercase tracking-wider">
                  {unreadDivider.count} unread {unreadDivider.count === 1 ? 'message' : 'messages'}
                </span>
                <div className="h-px flex-1 bg-emerald-500/30" />
              </div>
            )}
            <div className={sep ? '' : firstInGroup ? 'mt-2' : 'mt-0.5'}>
              <MessageBubble
                msg={m}
                group={group}
                nameMap={nameMap}
                mentionIndex={mentionIndex}
                onOpenTask={onOpenTask}
                onTasksChanged={onTasksChanged}
                onOpenChat={onOpenChat}
                onReply={onReply}
                onReact={onReact}
                onForward={onForward}
                onStar={onStar}
                onEdit={onEdit}
                onOpenImage={onOpenImage}
                onJumpToMessage={onJumpToMessage}
                selfDigits={selfDigits}
                firstInGroup={firstInGroup}
                highlighted={highlightId === m.id}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}

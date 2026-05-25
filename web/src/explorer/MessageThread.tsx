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
    setReplyTo(null)
    setLightboxIdx(null)
    setForwardMsg(null)
  }, [jid])

  // Append a live message that belongs to this chat.
  useEffect(() => {
    if (!liveMsg || liveMsg.chat_jid !== jid) return
    setMessages((prev) => {
      if (prev.some((m) => m.id === liveMsg.id)) return prev
      return [...prev, liveMsg]
    })
  }, [liveMsg, jid])

  // Keep the view pinned to the newest message after loads/appends.
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight
  }, [messages, loading])

  function onScroll() {
    const el = scrollRef.current
    if (!el) return
    stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
  }

  function loadEarlier() {
    stickToBottom.current = false
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

  // Append a just-sent message locally. Sent messages go straight through the
  // send API and are not echoed over the SSE stream, so we add them here.
  function handleSent(m: Message) {
    stickToBottom.current = true
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
        <ChatCircles jid={jid} circles={circles} onChanged={onCirclesChanged} />
      </header>

      {(group || isContact) && <ProfileCard type={group ? 'group' : 'contact'} ref_={jid} />}

      <div
        ref={scrollRef}
        onScroll={onScroll}
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
        className="relative min-h-0 flex-1 overflow-y-auto px-4 py-4"
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
              onReply={canSend ? setReplyTo : undefined}
              onReact={canSend ? handleReact : undefined}
              onForward={setForwardMsg}
              onStar={handleStar}
              onOpenImage={openLightboxFor}
              selfDigits={selfDigits}
            />
          </>
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
          nameMap={nameMap}
          onClearReply={() => setReplyTo(null)}
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
  nameMap,
  onClearReply,
  onDraftConsumed,
  onSent,
  setAttachmentRef,
}: {
  jid: string
  group: boolean
  initialText?: string
  replyTo?: Message | null
  nameMap?: Map<string, string>
  onClearReply?: () => void
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
  const taRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
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

  async function send() {
    const body = text.trim()
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

  const canSendNow = (text.trim().length > 0 || attachment !== null) && !sending

  return (
    <div className="border-t border-neutral-800 px-4 py-3">
      {error && <div className="mb-1 text-xs text-red-400">{error}</div>}
      {replyTo && (
        <ReplyQuote
          msg={replyTo}
          nameMap={nameMap || new Map()}
          onClear={() => onClearReply?.()}
        />
      )}
      {attachment && (
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
          title="Attach a file"
          aria-label="Attach a file"
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M21.44 11.05 12.25 20.24a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66L9.41 17.41a2 2 0 0 1-2.83-2.83l8.49-8.49" />
          </svg>
        </button>
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
            }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            onSelect={(e) => updateMentionContext(text, (e.target as HTMLTextAreaElement).selectionStart)}
            placeholder={attachment ? 'Add a caption…' : 'Type a message'}
            className="max-h-40 min-h-[2.5rem] w-full resize-none rounded-2xl border border-neutral-800 bg-neutral-900 px-3 py-2 text-sm outline-none placeholder:text-neutral-600 focus:border-neutral-600"
          />
        </div>
        <button
          onClick={send}
          disabled={!canSendNow}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
          title={uploading ? 'Uploading…' : 'Send'}
        >
          {sending ? '…' : '➤'}
        </button>
      </div>
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
  onOpenImage,
  selfDigits,
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
  onOpenImage?: (msg: Message) => void
  selfDigits?: Set<string>
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
          <div key={m.id + m.timestamp}>
            {sep && (
              <div className="my-3 flex justify-center">
                <span className="rounded-full bg-neutral-800 px-3 py-1 text-[11px] text-neutral-400">
                  {day}
                </span>
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
                onOpenImage={onOpenImage}
                selfDigits={selfDigits}
                firstInGroup={firstInGroup}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}

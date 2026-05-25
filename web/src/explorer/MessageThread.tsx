import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, type Chat, type Circle, type Message, type Tag } from '../api'
import {
  chatTitle, dayLabel, isGroup, isNewsletter, isStatus, jidUser, senderTitle,
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

const PAGE = 100

// MessageThread loads and renders the conversation for one chat, appends live
// messages, and supports loading earlier history.
export function MessageThread({
  jid,
  chats,
  nameMap,
  mentionIndex,
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
  const scrollRef = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)

  const chat = chats.find((c) => c.jid === jid)
  const title = chat ? chatTitle(chat, nameMap) : '+' + jidUser(jid)
  const group = isGroup(jid)
  const isContact = !group && !isStatus(jid) && !isNewsletter(jid)

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

  // Reset page size + clear any reply context when switching chats.
  useEffect(() => {
    setLimit(PAGE)
    stickToBottom.current = true
    setReplyTo(null)
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
          <div className="truncate text-xs text-neutral-500">
            {group ? 'Group' : jid.replace('@s.whatsapp.net', '')}
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

      <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
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
            />
          </>
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
}: {
  jid: string
  group: boolean
  initialText?: string
  replyTo?: Message | null
  nameMap?: Map<string, string>
  onClearReply?: () => void
  onDraftConsumed?: () => void
  onSent: (m: Message) => void
}) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const taRef = useRef<HTMLTextAreaElement>(null)

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
    if (!body || sending) return
    setSending(true)
    setError('')
    try {
      // Route through reply when a quote target is set, otherwise the regular
      // send endpoint. Both return {message_id, timestamp}.
      const res = replyTo
        ? await api.reply(jid, replyTo.id, body)
        : await api.send(jid, body)
      // Echo locally with reply_to_* populated so the new bubble shows its
      // quote bar immediately, without waiting for an SSE round-trip.
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
        message_type: 'text',
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
      requestAnimationFrame(resize)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to send')
    } finally {
      setSending(false)
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

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
      <div className="flex items-end gap-2">
        <textarea
          ref={taRef}
          dir="auto"
          rows={1}
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            resize()
          }}
          onKeyDown={onKeyDown}
          placeholder="Type a message"
          className="max-h-40 min-h-[2.5rem] flex-1 resize-none rounded-2xl border border-neutral-800 bg-neutral-900 px-3 py-2 text-sm outline-none placeholder:text-neutral-600 focus:border-neutral-600"
        />
        <button
          onClick={send}
          disabled={!text.trim() || sending}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-neutral-950 transition hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
          title="Send"
        >
          {sending ? '…' : '➤'}
        </button>
      </div>
    </div>
  )
}

// ReplyQuote is the small chip the Composer renders above its textarea while a
// reply target is staged — mirrors the official WA "you're replying to X"
// preview: colored vertical stripe, sender name in their per-sender color, a
// single-line snippet of the quoted body, and an × to cancel.
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
}: {
  messages: Message[]
  group: boolean
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChat?: (jid: string) => void
  onReply?: (msg: Message) => void
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
                firstInGroup={firstInGroup}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}

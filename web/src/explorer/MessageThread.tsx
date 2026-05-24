import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, type Chat, type Circle, type Message, type Tag } from '../api'
import { chatTitle, dayLabel, isGroup, isNewsletter, isStatus, jidUser } from './format'
import { MessageBubble } from './MessageBubble'
import { ChatCircles } from './ChatCircles'
import { TagChips, TagEditor } from './Tags'
import { ExtractionsModal } from './Extractions'
import { ProfileCard } from './ProfileCard'

const PAGE = 100

// MessageThread loads and renders the conversation for one chat, appends live
// messages, and supports loading earlier history.
export function MessageThread({
  jid,
  chats,
  nameMap,
  liveMsg,
  circles,
  allTags,
  contactTags,
  onCirclesChanged,
  onTagsChanged,
  onOpenTask,
  onTasksChanged,
  onOpenChatTasks,
  onSent,
}: {
  jid: string
  chats: Chat[]
  nameMap: Map<string, string>
  liveMsg: Message | null
  circles: Circle[]
  allTags: Tag[]
  contactTags: Record<string, Tag[]>
  onCirclesChanged: () => void
  onTagsChanged: () => void
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
  onOpenChatTasks: (jid: string) => void
  onSent?: (m: Message) => void
}) {
  const [messages, setMessages] = useState<Message[]>([])
  const [limit, setLimit] = useState(PAGE)
  const [loading, setLoading] = useState(true)
  const [hasMore, setHasMore] = useState(false)
  const [extracting, setExtracting] = useState(false)
  const [showHistory, setShowHistory] = useState(false)
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

  // Reset page size when switching chats.
  useEffect(() => {
    setLimit(PAGE)
    stickToBottom.current = true
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
        <div
          className={
            'flex h-9 w-9 items-center justify-center rounded-full text-sm font-semibold ' +
            (group ? 'bg-sky-600/30 text-sky-300' : 'bg-neutral-700 text-neutral-200')
          }
        >
          {(title.replace(/^\+/, '')[0] || '?').toUpperCase()}
        </div>
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
                onTasksChanged()
                onOpenChatTasks(jid)
                alert(`Extracted ${r.created} task(s).\n\n${r.summary || ''}`)
              } catch (e) {
                alert('Extraction failed: ' + (e as Error).message)
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
              onOpenTask={onOpenTask}
              onTasksChanged={onTasksChanged}
            />
          </>
        )}
      </div>

      {canSend && <Composer jid={jid} group={group} onSent={handleSent} />}

      {showHistory && (
        <ExtractionsModal
          title={title}
          fetchRuns={() => api.listExtractions(jid)}
          onClose={() => setShowHistory(false)}
        />
      )}
    </div>
  )
}

// Composer is the message input at the bottom of a thread. Enter sends,
// Shift+Enter inserts a newline. dir="auto" keeps Arabic/English typing aligned.
function Composer({
  jid,
  group,
  onSent,
}: {
  jid: string
  group: boolean
  onSent: (m: Message) => void
}) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const taRef = useRef<HTMLTextAreaElement>(null)

  function resize() {
    const el = taRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }

  async function send() {
    const body = text.trim()
    if (!body || sending) return
    setSending(true)
    setError('')
    try {
      const res = await api.send(jid, body)
      onSent({
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
      })
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

// Timeline renders bubbles with day separators between different dates.
function Timeline({
  messages,
  group,
  nameMap,
  onOpenTask,
  onTasksChanged,
}: {
  messages: Message[]
  group: boolean
  nameMap: Map<string, string>
  onOpenTask: (id: number) => void
  onTasksChanged: () => void
}) {
  let lastDay = ''
  return (
    <div className="flex flex-col gap-1">
      {messages.map((m) => {
        const day = dayLabel(m.timestamp)
        const sep = day !== lastDay
        lastDay = day
        return (
          <div key={m.id + m.timestamp}>
            {sep && (
              <div className="my-3 flex justify-center">
                <span className="rounded-full bg-neutral-800 px-3 py-1 text-[11px] text-neutral-400">
                  {day}
                </span>
              </div>
            )}
            <MessageBubble
              msg={m}
              group={group}
              nameMap={nameMap}
              onOpenTask={onOpenTask}
              onTasksChanged={onTasksChanged}
            />
          </div>
        )
      })}
    </div>
  )
}

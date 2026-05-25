import { useEffect, useRef, useState } from 'react'
import type { Message } from '../api'
import { clockTime, humanSize, mediaURL, senderTitle, type MentionEntry } from './format'
import { senderColor } from './colors'
import { ChatAvatar } from './ChatAvatar'
import { MessageTaskButton } from './MessageTaskButton'
import { RichText } from './RichText'
import { EmojiPicker } from './EmojiPicker'

// MessageBubble renders one message: alignment, sender (in groups), reply
// preview, media, text, reactions, and edited/deleted/forwarded markers.
// Body text is run through RichText so URLs become clickable and "@<digits>"
// mentions resolve to contact names + click → open DM.
export function MessageBubble({
  msg,
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
  onInfo,
  onOpenImage,
  onJumpToMessage,
  selfDigits,
  firstInGroup = true,
  highlighted = false,
  highlightQuery,
}: {
  msg: Message
  group: boolean
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  /** Digit identifiers that map to the current user. Forwarded to RichText
   *  so 'mention chips that ping you' render in emerald; also used here to
   *  add a faint emerald ring around the whole bubble when you're pinged. */
  selfDigits?: Set<string>
  onOpenTask?: (id: number) => void
  onTasksChanged?: () => void
  onOpenChat?: (jid: string) => void
  onReply?: (msg: Message) => void
  /** Called when the user picks an emoji from the quick-react popover.
   *  Empty string removes any existing reaction (WA's toggle semantics). */
  onReact?: (msg: Message, emoji: string) => void
  /** Called when the user clicks the Forward action — lifts to the thread
   *  which opens the multi-target share-sheet picker. */
  onForward?: (msg: Message) => void
  /** Toggle the local star bookmark on this message. */
  onStar?: (msg: Message, starred: boolean) => void
  /** Called when the Edit action is clicked on one of your own bubbles —
   *  lifts to the thread which puts the composer into edit mode for this
   *  message. Only wired through for text messages within WA's 15-minute
   *  edit window; otherwise the bubble suppresses the action itself. */
  onEdit?: (msg: Message) => void
  /** Called when the Info action is clicked on one of your own bubbles —
   *  lifts to the thread which opens the Message Info overlay with
   *  per-recipient delivered + read timestamps. */
  onInfo?: (msg: Message) => void
  /** Called when an image bubble is clicked — lifts to the thread which
   *  opens the in-app lightbox at this message's position. */
  onOpenImage?: (msg: Message) => void
  /** Called when the quoted-reply preview chip is clicked — lifts to the
   *  thread, which jumps to the original message and briefly flashes it.
   *  No-ops silently if the original isn't in the loaded window. */
  onJumpToMessage?: (id: string) => void
  /** When false, this message is a continuation of the previous sender's burst
   *  — the sender label is suppressed, matching the official WA "clustering"
   *  rule where the name only shows on the first bubble of a streak. */
  firstInGroup?: boolean
  /** Visually emphasised because it's the current hit of an in-chat search.
   *  Adds a strong amber ring around the bubble; MessageThread scrolls it
   *  into view as the user steps ↑/↓ through matches. */
  highlighted?: boolean
  /** When set, body text + caption + AI transcripts wrap matches of this
   *  string in an amber <mark> so the user sees exactly where the search
   *  hit lives inside the bubble. */
  highlightQuery?: string
}) {
  const mine = msg.is_from_me
  const sender =
    group && !mine && firstInGroup
      ? senderTitle(msg.sender, msg.sender_name, msg.push_name, nameMap)
      : ''
  // Resolved sender title used for the avatar's letter fallback. Outside the
  // firstInGroup gate above because continuation bubbles (no sender label)
  // still want a stable fallback, even though they render only a spacer.
  const senderFull = group && !mine
    ? senderTitle(msg.sender, msg.sender_name, msg.push_name, nameMap)
    : ''

  // True when an incoming message mentions the current user — used to put a
  // soft emerald ring around the bubble so a ping in a busy thread stands
  // out even before you've parsed the body. Skips outgoing messages (you
  // can't @-mention yourself in any useful way) and any message without a
  // self-digits set wired in.
  const selfMentioned =
    !mine &&
    !!selfDigits?.size &&
    !!(msg.content || msg.media_caption) &&
    hasSelfMention(msg.content || msg.media_caption || '', selfDigits)

  // Group bubble avatar slot: real circular avatar on the first bubble of a
  // sender's cluster, invisible spacer on continuations so the bubbles stay
  // left-aligned with the avatar. Only for incoming group messages — DMs
  // never need it (the chat header already shows who you're talking to)
  // and outgoing bubbles never get a sender avatar.
  const showAvatarSlot = group && !mine

  // Edit is only meaningful for our own text messages within WhatsApp's
  // 15-minute server window. Media captions can't be edited via /edit (the
  // bridge wraps new_text in Conversation, not the image/video message), and
  // edits past the window just fail loudly — better to hide the action than
  // surprise the user. canEdit gates the pencil button on BubbleActions.
  const EDIT_WINDOW_S = 15 * 60
  const canEdit =
    mine &&
    !msg.is_deleted &&
    !msg.media_type &&
    !!msg.content &&
    Math.floor(Date.now() / 1000) - msg.timestamp < EDIT_WINDOW_S

  // The "promote to task" action is bridge-specific (not a WA feature) but
  // we render it inside the same gutter cluster as the other actions so the
  // bubble has one visual language. The bubble decides whether to offer it
  // based on whether the parent passed an onOpenTask handler (the dashboard
  // view does; some read-only contexts don't).
  const taskButton =
    onOpenTask && !msg.is_deleted ? (
      <MessageTaskButton
        chatJID={msg.chat_jid}
        messageID={msg.id}
        defaultTitle={msg.content || msg.media_caption || ''}
        onOpenTask={onOpenTask}
        onChanged={onTasksChanged || (() => {})}
        side={mine ? 'left' : 'right'}
      />
    ) : null

  return (
    <div className={'group/row flex items-end gap-2 ' + (mine ? 'justify-end' : 'justify-start')}>
      {showAvatarSlot && (
        firstInGroup ? (
          <ChatAvatar jid={msg.sender} title={senderFull} size={28} clickable />
        ) : (
          <div className="w-7 shrink-0" aria-hidden="true" />
        )
      )}
      {/* Reply action hovers on the left of outgoing bubbles / right of incoming
          (so it sits between the bubble and the chat edge, exactly where the
          official WA chevron lives). Only rendered when the chat can be replied
          to (onReply provided) and the message isn't already deleted. */}
      {(onReply || onReact || onForward || onStar || (onEdit && canEdit) || onInfo || taskButton) && mine && !msg.is_deleted && (
        <BubbleActions
          onReply={onReply ? () => onReply(msg) : undefined}
          onReact={onReact ? (emoji) => onReact(msg, emoji) : undefined}
          onForward={onForward ? () => onForward(msg) : undefined}
          onStar={onStar ? () => onStar(msg, !msg.is_starred) : undefined}
          onEdit={onEdit && canEdit ? () => onEdit(msg) : undefined}
          onInfo={onInfo ? () => onInfo(msg) : undefined}
          taskButton={taskButton}
          isStarred={!!msg.is_starred}
          side="left"
        />
      )}
      <div
        className={
          'group max-w-[78%] rounded-2xl px-3 py-2 text-sm transition ' +
          (mine ? 'bg-emerald-700/40' : 'bg-neutral-800') +
          (highlighted
            ? ' ring-2 ring-amber-400/80 shadow-lg shadow-amber-500/20'
            : selfMentioned ? ' ring-1 ring-emerald-400/60' : '')
        }
      >
        {sender && (
          // Per-sender color (stable hash of sender JID) — mirrors WhatsApp's
          // "person color" so speakers stay visually distinct in busy groups.
          <div
            dir="auto"
            className="mb-1 text-xs font-semibold"
            style={{ color: senderColor(msg.sender) }}
          >
            {sender}
          </div>
        )}

        {msg.is_forwarded && <div className="mb-1 text-[11px] italic text-neutral-400">↪ Forwarded</div>}

        {msg.reply_to_content && (
          // Quoted-reply chip — clickable when we know which message it's
          // quoting AND the thread provided a jump handler. WA's mobile UX
          // is identical: tap the chip → original message scrolls into
          // view with a quick flash. The button takes the full width of the
          // chip so the entire area is the target, not just the text.
          onJumpToMessage && msg.reply_to_id ? (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onJumpToMessage(msg.reply_to_id!)
              }}
              title="Jump to the original message"
              className="mb-1 block w-full border-l-2 border-emerald-400/60 bg-black/20 px-2 py-1 text-start text-xs text-neutral-300 transition hover:bg-black/40"
            >
              <div dir="auto" className="line-clamp-2">
                {msg.reply_to_content}
              </div>
            </button>
          ) : (
            <div className="mb-1 border-l-2 border-emerald-400/60 bg-black/20 px-2 py-1 text-xs text-neutral-300">
              <div dir="auto" className="line-clamp-2 text-start">
                {msg.reply_to_content}
              </div>
            </div>
          )
        )}

        {msg.is_deleted ? (
          <div className="italic text-neutral-500">🚫 This message was deleted</div>
        ) : (
          <>
            <MediaContent msg={msg} onOpenImage={onOpenImage} />
            <TextContent
              msg={msg}
              mentionIndex={mentionIndex}
              onOpenChat={onOpenChat}
              selfDigits={selfDigits}
              highlightQuery={highlightQuery}
            />
            <MediaUnderstanding
              msg={msg}
              mine={mine}
              mentionIndex={mentionIndex}
              onOpenChat={onOpenChat}
              selfDigits={selfDigits}
              highlightQuery={highlightQuery}
            />
          </>
        )}

        {msg.reactions && msg.reactions.length > 0 && (
          <ReactionChips reactions={msg.reactions} />
        )}

        <div className="mt-1 flex items-center justify-end gap-1 text-[10px] text-neutral-400">
          {msg.is_edit && <span className="italic">edited</span>}
          {msg.is_starred && (
            <span title="Starred" aria-label="Starred" className="text-amber-300">
              <svg viewBox="0 0 24 24" width="11" height="11" fill="currentColor">
                <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
              </svg>
            </span>
          )}
          <span>{clockTime(msg.timestamp)}</span>
          {mine && <StatusTicks status={msg.status} />}
        </div>
      </div>
      {(onReply || onReact || onForward || onStar || taskButton) && !mine && !msg.is_deleted && (
        <BubbleActions
          onReply={onReply ? () => onReply(msg) : undefined}
          onReact={onReact ? (emoji) => onReact(msg, emoji) : undefined}
          onForward={onForward ? () => onForward(msg) : undefined}
          onStar={onStar ? () => onStar(msg, !msg.is_starred) : undefined}
          taskButton={taskButton}
          isStarred={!!msg.is_starred}
          side="right"
        />
      )}
    </div>
  )
}

// BubbleActions is the hover-only cluster of small circular buttons that sit
// outside the bubble on its gutter side, mirroring official WA's position for
// reply / react / more. We render reply first, then a smile that toggles the
// quick-react popover. The popover is anchored to the smile button and shows
// WA's classic 6 quick reactions.
function BubbleActions({
  onReply,
  onReact,
  onForward,
  onStar,
  onEdit,
  onInfo,
  taskButton,
  isStarred,
  side,
}: {
  onReply?: () => void
  onReact?: (emoji: string) => void
  onForward?: () => void
  onStar?: () => void
  /** Show the Edit pencil. Only passed in for own text messages within
   *  WA's 15-minute window — gating happens in MessageBubble. */
  onEdit?: () => void
  /** Show the Info (ℹ) button. Only passed in for own messages —
   *  opens the per-recipient delivered/read receipts overlay. */
  onInfo?: () => void
  /** Pre-rendered "promote to task" button (MessageTaskButton). Lives in the
   *  same gutter cluster so it reads as a sibling action, not a stray icon
   *  in the footer. The bubble builds it with full context (chatJID, etc.). */
  taskButton?: React.ReactNode
  isStarred?: boolean
  side: 'left' | 'right'
}) {
  const [pickerOpen, setPickerOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)

  // Click outside the action cluster closes the picker.
  useEffect(() => {
    if (!pickerOpen) return
    function onDown(e: MouseEvent) {
      if (!wrapRef.current?.contains(e.target as Node)) setPickerOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [pickerOpen])

  // Always-visible cluster while the picker is open — otherwise it fades out
  // the moment you move the cursor toward the popover.
  const visibility = pickerOpen ? 'opacity-100' : 'opacity-0 group-hover/row:opacity-100'

  return (
    <div ref={wrapRef} className={`relative flex shrink-0 items-center gap-1 ${visibility} transition`}>
      {onReply && <ReplyButton onClick={onReply} />}
      {onReact && (
        <button
          onClick={() => setPickerOpen((v) => !v)}
          title="React"
          aria-label="React"
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-300 transition hover:bg-neutral-700 hover:text-neutral-100"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M8 14s1.5 2 4 2 4-2 4-2" />
            <circle cx="9" cy="10" r="0.6" fill="currentColor" />
            <circle cx="15" cy="10" r="0.6" fill="currentColor" />
          </svg>
        </button>
      )}
      {onForward && (
        <button
          onClick={onForward}
          title="Forward"
          aria-label="Forward"
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-300 transition hover:bg-neutral-700 hover:text-neutral-100"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 17 20 12 15 7" />
            <path d="M4 18v-2a4 4 0 0 1 4-4h12" />
          </svg>
        </button>
      )}
      {onStar && (
        <button
          onClick={onStar}
          title={isStarred ? 'Unstar' : 'Star'}
          aria-label={isStarred ? 'Unstar' : 'Star'}
          className={
            'flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition ' +
            (isStarred
              ? 'bg-amber-500/20 text-amber-300 hover:bg-amber-500/30'
              : 'bg-neutral-800/80 text-neutral-300 hover:bg-neutral-700 hover:text-neutral-100')
          }
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill={isStarred ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
          </svg>
        </button>
      )}
      {onEdit && (
        <button
          onClick={onEdit}
          title="Edit"
          aria-label="Edit"
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-300 transition hover:bg-neutral-700 hover:text-neutral-100"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 20h9" />
            <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" />
          </svg>
        </button>
      )}
      {onInfo && (
        <button
          onClick={onInfo}
          title="Message info — delivered + read receipts"
          aria-label="Message info"
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-300 transition hover:bg-neutral-700 hover:text-neutral-100"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="16" x2="12" y2="12" />
            <line x1="12" y1="8" x2="12.01" y2="8" />
          </svg>
        </button>
      )}
      {taskButton}
      {pickerOpen && onReact && (
        <ReactionPicker
          side={side}
          onPick={(emoji) => {
            setPickerOpen(false)
            onReact(emoji)
          }}
        />
      )}
    </div>
  )
}

// ReactionChips aggregates the per-reactor rows into one chip per emoji with
// a count (when more than one person reacted with it) — matching official
// WA's reaction display. The chip the current user added is tinted emerald
// so they can spot their own reaction at a glance.
function ReactionChips({ reactions }: { reactions: NonNullable<Message['reactions']> }) {
  // Group by emoji, count, and remember whether the user themself reacted
  // with that emoji. '__me__' is the sentinel handleReact assigns for the
  // optimistic local update — server-pulled reactions don't carry it, so a
  // page refresh currently loses the "mine" highlight (acceptable tradeoff
  // until the bridge exposes self-JID).
  type Agg = { emoji: string; count: number; mine: boolean }
  const map = new Map<string, Agg>()
  for (const r of reactions) {
    if (!r.emoji) continue
    const cur = map.get(r.emoji) || { emoji: r.emoji, count: 0, mine: false }
    cur.count += 1
    if (r.sender === '__me__') cur.mine = true
    map.set(r.emoji, cur)
  }
  const aggregated = Array.from(map.values())
  if (aggregated.length === 0) return null
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {aggregated.map(({ emoji, count, mine }) => (
        <span
          key={emoji}
          className={
            'flex items-center rounded-full px-1.5 py-0.5 text-xs ring-1 ' +
            (mine
              ? 'bg-emerald-500/20 text-emerald-100 ring-emerald-500/40'
              : 'bg-black/30 text-neutral-100 ring-black/0')
          }
        >
          <span className="leading-none">{emoji}</span>
          {count > 1 && (
            <span className="ml-1 text-[10px] tabular-nums opacity-80">{count}</span>
          )}
        </span>
      ))}
    </div>
  )
}

const QUICK_REACTIONS = ['👍', '❤️', '😂', '😮', '😢', '🙏']

// ReactionPicker pops above the smile button and lays the 6 quick reactions
// in a single row, exactly like WhatsApp's quick-react bar. The trailing
// "+" button promotes the popover to the full categorized picker — also
// WA: tap the + → any emoji as a reaction, not just the six quick ones.
function ReactionPicker({
  side,
  onPick,
}: {
  side: 'left' | 'right'
  onPick: (emoji: string) => void
}) {
  const [showFull, setShowFull] = useState(false)
  return (
    <>
      <div
        className={
          'absolute bottom-full mb-1 flex gap-0.5 rounded-full bg-neutral-900 px-1.5 py-1 shadow-lg ring-1 ring-neutral-700 ' +
          (side === 'left' ? 'right-0' : 'left-0')
        }
      >
        {QUICK_REACTIONS.map((e) => (
          <button
            key={e}
            onClick={() => onPick(e)}
            className="flex h-7 w-7 items-center justify-center rounded-full text-base leading-none transition hover:scale-125 hover:bg-neutral-800"
            title={e}
          >
            {e}
          </button>
        ))}
        <button
          onClick={(e) => { e.stopPropagation(); setShowFull(true) }}
          title="More emoji…"
          aria-label="Open full emoji picker"
          className="flex h-7 w-7 items-center justify-center rounded-full text-sm text-neutral-400 leading-none transition hover:scale-125 hover:bg-neutral-800 hover:text-neutral-100"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
        </button>
      </div>
      {showFull && (
        <EmojiPicker
          mode="modal"
          onPick={(emoji) => { onPick(emoji) /* parent closes quick popover */ }}
          onClose={() => setShowFull(false)}
        />
      )}
    </>
  )
}

// ReplyButton: small circular hover-only action that mirrors WhatsApp's reply
// chevron. Hidden until you hover the row; clicking sets the row's message as
// the composer's reply target.
function ReplyButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      title="Reply"
      aria-label="Reply"
      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-neutral-800/80 text-neutral-400 opacity-0 transition hover:bg-neutral-700 hover:text-neutral-100 group-hover/row:opacity-100"
    >
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M9 17 4 12 9 7" />
        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
      </svg>
    </button>
  )
}

// TextContent shows message text, but not the "[image:…]" placeholder we store
// for pure-media messages (the media itself + caption already cover it).
function TextContent({
  msg,
  mentionIndex,
  onOpenChat,
  selfDigits,
  highlightQuery,
}: {
  msg: Message
  mentionIndex: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
  selfDigits?: Set<string>
  highlightQuery?: string
}) {
  if (msg.media_type) {
    const caption = msg.media_caption
    return caption ? (
      <RichText text={caption} mentions={mentionIndex} onOpenChat={onOpenChat} selfDigits={selfDigits} highlightQuery={highlightQuery} />
    ) : null
  }
  if (!msg.content) return null
  return <RichText text={msg.content} mentions={mentionIndex} onOpenChat={onOpenChat} selfDigits={selfDigits} highlightQuery={highlightQuery} />
}

function MediaContent({
  msg,
  onOpenImage,
}: {
  msg: Message
  onOpenImage?: (msg: Message) => void
}) {
  if (!msg.media_type) return null
  // Pass chat_jid so mediaURL can append ?unlock=… when the chat is open via
  // a per-chat fingerprint unlock (audio/img tags can't carry headers).
  const url = mediaURL(msg.media_path, msg.chat_jid)

  // Media exists but was not downloaded (policy/size/expired link).
  if (!url) {
    return (
      <div className="mb-1 flex items-center gap-2 rounded-lg bg-black/20 px-3 py-2 text-xs text-neutral-400">
        <span>📎</span>
        <span>
          {label(msg.media_type)} not downloaded
          {msg.media_size ? ` · ${humanSize(msg.media_size)}` : ''}
        </span>
      </div>
    )
  }

  switch (msg.media_type) {
    case 'image':
      // Click opens the in-app lightbox when onOpenImage is wired (the chat
      // thread), otherwise falls back to a new tab so contexts that render
      // bubbles without a thread (briefings, search snippets) still work.
      return onOpenImage ? (
        <button
          type="button"
          onClick={() => onOpenImage(msg)}
          className="mb-1 block max-w-full cursor-zoom-in p-0"
        >
          <img src={url} alt="" className="max-h-80 rounded-lg object-contain" />
        </button>
      ) : (
        <a href={url} target="_blank" rel="noreferrer">
          <img src={url} alt="" className="mb-1 max-h-80 rounded-lg object-contain" />
        </a>
      )
    case 'sticker':
      return <img src={url} alt="sticker" className="mb-1 h-28 w-28 object-contain" />
    case 'video':
      return <video src={url} controls className="mb-1 max-h-80 rounded-lg" />
    case 'voice_note':
    case 'audio':
      return <VoiceBubble msg={msg} url={url} />
    case 'document':
      return (
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          className="mb-1 flex items-center gap-2 rounded-lg bg-black/20 px-3 py-2 text-xs hover:bg-black/30"
        >
          <span>📄</span>
          <span className="truncate">{msg.media_filename || 'Document'}</span>
          {msg.media_size ? <span className="text-neutral-500">{humanSize(msg.media_size)}</span> : null}
        </a>
      )
    default:
      return null
  }
}

// VoiceBubble plays a voice note / audio file in a row that matches the
// official WA voice-message UX: a round play/pause button, a thin
// scrubbable progress bar with a draggable thumb, a 1× / 1.5× / 2× speed
// toggle, and a tabular-nums duration counter. The native <audio> element
// is hidden — we drive it through refs and reflect its state into the
// custom UI, so seek / pause / resume work without showing the browser's
// chrome.
function VoiceBubble({ msg, url }: { msg: Message; url: string }) {
  const audioRef = useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = useState(false)
  const [duration, setDuration] = useState(0)
  const [current, setCurrent] = useState(0)
  const [speed, setSpeed] = useState(1)
  const mine = msg.is_from_me

  // Wire the hidden audio element to local state. We can't trust the parent
  // re-rendering to keep playing/current in sync — the user can pause /
  // resume / scrub between renders, so we always read the live element.
  useEffect(() => {
    const a = audioRef.current
    if (!a) return
    const onPlay = () => setPlaying(true)
    const onPause = () => setPlaying(false)
    const onEnded = () => {
      setPlaying(false)
      setCurrent(0)
    }
    const onTime = () => setCurrent(a.currentTime)
    const onMeta = () => setDuration(a.duration)
    a.addEventListener('play', onPlay)
    a.addEventListener('pause', onPause)
    a.addEventListener('ended', onEnded)
    a.addEventListener('timeupdate', onTime)
    a.addEventListener('loadedmetadata', onMeta)
    return () => {
      a.removeEventListener('play', onPlay)
      a.removeEventListener('pause', onPause)
      a.removeEventListener('ended', onEnded)
      a.removeEventListener('timeupdate', onTime)
      a.removeEventListener('loadedmetadata', onMeta)
    }
  }, [])

  function toggle() {
    const a = audioRef.current
    if (!a) return
    if (a.paused) void a.play()
    else a.pause()
  }

  // Cycle 1× → 1.5× → 2× → 1× — matches WA's speed toggle behavior.
  function cycleSpeed() {
    const next = speed === 1 ? 1.5 : speed === 1.5 ? 2 : 1
    setSpeed(next)
    if (audioRef.current) audioRef.current.playbackRate = next
  }

  function seek(e: React.MouseEvent<HTMLDivElement>) {
    const a = audioRef.current
    if (!a || !duration || !isFinite(duration)) return
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    a.currentTime = ratio * duration
  }

  // WA shows the total duration before playing / after ending, and the
  // elapsed time while mid-play.
  const showElapsed = playing || (current > 0 && current < duration)
  const displaySec = showElapsed ? current : duration
  const pct = duration > 0 ? (current / duration) * 100 : 0

  // Color scheme adapts to bubble side so the play button + progress fill
  // stay readable inside both the emerald (mine) and neutral (theirs) bubble.
  const playBg = mine ? 'bg-emerald-500 text-neutral-950' : 'bg-neutral-700 text-neutral-100'
  const trackBg = mine ? 'bg-emerald-200/30' : 'bg-neutral-600/60'
  const fillBg = mine ? 'bg-emerald-200' : 'bg-neutral-300'
  const speedColor = mine ? 'text-emerald-100' : 'text-neutral-300'

  return (
    <div className="mb-1 flex w-64 max-w-full items-center gap-3">
      <button
        onClick={toggle}
        title={playing ? 'Pause' : 'Play'}
        aria-label={playing ? 'Pause' : 'Play'}
        className={'flex h-9 w-9 shrink-0 items-center justify-center rounded-full transition ' + playBg}
      >
        {playing ? (
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <rect x="6" y="5" width="4" height="14" rx="1" />
            <rect x="14" y="5" width="4" height="14" rx="1" />
          </svg>
        ) : (
          <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
            <path d="M8 5v14l11-7z" />
          </svg>
        )}
      </button>
      <div className="min-w-0 flex-1">
        <div
          onClick={seek}
          className={'relative h-1 cursor-pointer rounded-full ' + trackBg}
        >
          <div
            className={'absolute inset-y-0 left-0 rounded-full ' + fillBg}
            style={{ width: pct + '%' }}
          />
          <div
            className={'absolute top-1/2 h-3 w-3 -translate-y-1/2 rounded-full shadow ' + fillBg}
            style={{ left: `calc(${pct}% - 6px)` }}
          />
        </div>
        <div className="mt-1 flex items-center justify-between text-[10px] tabular-nums text-neutral-400">
          <span>{fmtVoiceTime(displaySec)}</span>
          <button
            onClick={cycleSpeed}
            title="Playback speed"
            className={`rounded-full px-1.5 py-px text-[10px] font-semibold leading-none transition hover:bg-black/20 ${speedColor}`}
          >
            {speed === 1 ? '1×' : speed + '×'}
          </button>
        </div>
      </div>
      <audio ref={audioRef} src={url} preload="metadata" className="hidden" />
    </div>
  )
}

function fmtVoiceTime(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s < 10 ? '0' + s : s}`
}

// MediaUnderstanding renders the AI-derived transcript (for voice notes) or
// description (for images) as a smaller, dimmer footnote under the media,
// separated by a hairline divider. Hidden when no AI text exists.
function MediaUnderstanding({
  msg,
  mine,
  mentionIndex,
  onOpenChat,
  selfDigits,
  highlightQuery,
}: {
  msg: Message
  mine: boolean
  mentionIndex: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
  selfDigits?: Set<string>
  highlightQuery?: string
}) {
  const isAudio = msg.media_type === 'voice_note' || msg.media_type === 'audio'
  const text = isAudio ? msg.transcript : msg.media_description
  if (!text) return null
  const label = isAudio ? '🎙 Transcript' : '🖼 Description'
  return (
    <div
      className={
        'mt-1 border-t pt-1.5 text-[11px] leading-relaxed ' +
        (mine ? 'border-emerald-900/40 text-emerald-100/70' : 'border-neutral-700 text-neutral-400')
      }
    >
      <div className="mb-0.5 text-[10px] uppercase tracking-wider opacity-70">{label}</div>
      <RichText text={text} mentions={mentionIndex} onOpenChat={onOpenChat} selfDigits={selfDigits} highlightQuery={highlightQuery} />
    </div>
  )
}

// StatusTicks renders the WhatsApp delivery ticks shown after the timestamp on
// our own outgoing messages: single grey ✓ (sent), double grey ✓✓ (delivered),
// double blue ✓✓ (read or played). Hidden when status is unknown.
function StatusTicks({ status }: { status?: Message['status'] }) {
  if (!status) return null
  const isDouble = status !== 'sent'
  const isBlue = status === 'read' || status === 'played'
  // Blue mimics WA's #53bdeb; grey blends into the bubble footer.
  const color = isBlue ? '#53bdeb' : 'currentColor'
  const title =
    status === 'sent'
      ? 'Sent'
      : status === 'delivered'
        ? 'Delivered'
        : status === 'played'
          ? 'Played'
          : 'Read'
  return (
    <span title={title} aria-label={title} className="inline-flex items-center" style={{ color }}>
      <Tick />
      {isDouble && <Tick offset />}
    </span>
  )
}

// Tick is one checkmark, sized to the bubble footer. The `offset` variant is
// the trailing tick of a double-tick pair, nudged left so the two overlap the
// way the official WA ticks do.
function Tick({ offset = false }: { offset?: boolean }) {
  return (
    <svg
      viewBox="0 0 16 11"
      width="14"
      height="11"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={offset ? { marginLeft: '-7px' } : undefined}
      aria-hidden="true"
    >
      <path d="M1 6.5 L5 10.5 L15 0.5" />
    </svg>
  )
}

// hasSelfMention scans a message body for '@<digits>' tokens that match the
// current user. Same digit shape RichText already tokenises (7-16 digits),
// kept in sync so the bubble's emerald ring and the chip's emerald tint
// trigger on the exact same matches.
function hasSelfMention(text: string, self: Set<string>): boolean {
  if (!text || self.size === 0) return false
  const re = /@(\d{7,16})\b/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    if (self.has(m[1])) return true
  }
  return false
}

function label(type: string): string {
  switch (type) {
    case 'voice_note':
      return 'Voice note'
    case 'image':
      return 'Image'
    case 'video':
      return 'Video'
    case 'document':
      return 'Document'
    case 'sticker':
      return 'Sticker'
    default:
      return 'Media'
  }
}

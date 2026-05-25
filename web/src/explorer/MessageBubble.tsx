import { useEffect, useRef, useState } from 'react'
import type { Message } from '../api'
import { clockTime, humanSize, mediaURL, senderTitle, type MentionEntry } from './format'
import { senderColor } from './colors'
import { MessageTaskButton } from './MessageTaskButton'
import { RichText } from './RichText'

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
  onOpenImage,
  firstInGroup = true,
}: {
  msg: Message
  group: boolean
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
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
  /** Called when an image bubble is clicked — lifts to the thread which
   *  opens the in-app lightbox at this message's position. */
  onOpenImage?: (msg: Message) => void
  /** When false, this message is a continuation of the previous sender's burst
   *  — the sender label is suppressed, matching the official WA "clustering"
   *  rule where the name only shows on the first bubble of a streak. */
  firstInGroup?: boolean
}) {
  const mine = msg.is_from_me
  const sender =
    group && !mine && firstInGroup
      ? senderTitle(msg.sender, msg.sender_name, msg.push_name, nameMap)
      : ''

  return (
    <div className={'group/row flex items-center gap-1 ' + (mine ? 'justify-end' : 'justify-start')}>
      {/* Reply action hovers on the left of outgoing bubbles / right of incoming
          (so it sits between the bubble and the chat edge, exactly where the
          official WA chevron lives). Only rendered when the chat can be replied
          to (onReply provided) and the message isn't already deleted. */}
      {(onReply || onReact || onForward) && mine && !msg.is_deleted && (
        <BubbleActions
          onReply={onReply ? () => onReply(msg) : undefined}
          onReact={onReact ? (emoji) => onReact(msg, emoji) : undefined}
          onForward={onForward ? () => onForward(msg) : undefined}
          side="left"
        />
      )}
      <div
        className={
          'group max-w-[78%] rounded-2xl px-3 py-2 text-sm ' +
          (mine ? 'bg-emerald-700/40' : 'bg-neutral-800')
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
          <div className="mb-1 border-l-2 border-emerald-400/60 bg-black/20 px-2 py-1 text-xs text-neutral-300">
            <div dir="auto" className="line-clamp-2 text-start">
              {msg.reply_to_content}
            </div>
          </div>
        )}

        {msg.is_deleted ? (
          <div className="italic text-neutral-500">🚫 This message was deleted</div>
        ) : (
          <>
            <MediaContent msg={msg} onOpenImage={onOpenImage} />
            <TextContent msg={msg} mentionIndex={mentionIndex} onOpenChat={onOpenChat} />
            <MediaUnderstanding
              msg={msg}
              mine={mine}
              mentionIndex={mentionIndex}
              onOpenChat={onOpenChat}
            />
          </>
        )}

        {msg.reactions && msg.reactions.length > 0 && (
          <ReactionChips reactions={msg.reactions} />
        )}

        <div className="mt-1 flex items-center justify-end gap-1 text-[10px] text-neutral-400">
          {onOpenTask && (
            <MessageTaskButton
              chatJID={msg.chat_jid}
              messageID={msg.id}
              defaultTitle={msg.content || msg.media_caption || ''}
              onOpenTask={onOpenTask}
              onChanged={onTasksChanged || (() => {})}
            />
          )}
          {msg.is_edit && <span className="italic">edited</span>}
          <span>{clockTime(msg.timestamp)}</span>
          {mine && <StatusTicks status={msg.status} />}
        </div>
      </div>
      {(onReply || onReact || onForward) && !mine && !msg.is_deleted && (
        <BubbleActions
          onReply={onReply ? () => onReply(msg) : undefined}
          onReact={onReact ? (emoji) => onReact(msg, emoji) : undefined}
          onForward={onForward ? () => onForward(msg) : undefined}
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
  side,
}: {
  onReply?: () => void
  onReact?: (emoji: string) => void
  onForward?: () => void
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
// in a single row, exactly like WhatsApp's quick-react bar.
function ReactionPicker({
  side,
  onPick,
}: {
  side: 'left' | 'right'
  onPick: (emoji: string) => void
}) {
  return (
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
    </div>
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
}: {
  msg: Message
  mentionIndex: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
}) {
  if (msg.media_type) {
    const caption = msg.media_caption
    return caption ? (
      <RichText text={caption} mentions={mentionIndex} onOpenChat={onOpenChat} />
    ) : null
  }
  if (!msg.content) return null
  return <RichText text={msg.content} mentions={mentionIndex} onOpenChat={onOpenChat} />
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
}: {
  msg: Message
  mine: boolean
  mentionIndex: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
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
      <RichText text={text} mentions={mentionIndex} onOpenChat={onOpenChat} />
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

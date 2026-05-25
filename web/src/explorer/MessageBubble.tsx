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
}: {
  msg: Message
  group: boolean
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  onOpenTask?: (id: number) => void
  onTasksChanged?: () => void
  onOpenChat?: (jid: string) => void
  onReply?: (msg: Message) => void
}) {
  const mine = msg.is_from_me
  const sender = group && !mine ? senderTitle(msg.sender, msg.sender_name, msg.push_name, nameMap) : ''

  return (
    <div className={'group/row flex items-center gap-1 ' + (mine ? 'justify-end' : 'justify-start')}>
      {/* Reply action hovers on the left of outgoing bubbles / right of incoming
          (so it sits between the bubble and the chat edge, exactly where the
          official WA chevron lives). Only rendered when the chat can be replied
          to (onReply provided) and the message isn't already deleted. */}
      {onReply && mine && !msg.is_deleted && (
        <ReplyButton onClick={() => onReply(msg)} />
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
            <MediaContent msg={msg} />
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
          <div className="mt-1 flex flex-wrap gap-1">
            {msg.reactions.map((r, i) => (
              <span key={i} className="rounded-full bg-black/30 px-1.5 text-xs">
                {r.emoji}
              </span>
            ))}
          </div>
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
      {onReply && !mine && !msg.is_deleted && (
        <ReplyButton onClick={() => onReply(msg)} />
      )}
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

function MediaContent({ msg }: { msg: Message }) {
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
      return (
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
      return <audio src={url} controls className="mb-1 w-56 max-w-full" />
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

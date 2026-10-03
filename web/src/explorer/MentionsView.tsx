import { useCallback, useState } from 'react'
import { api, type Mention } from '../api'
import { usePoll } from '../hooks/usePoll'
import type { MentionEntry } from './format'
import { MessageBubble } from './MessageBubble'
import { QuickReplyPicker } from './QuickReplyPicker'

// The Mentions screen: every message where you were @-mentioned in a group,
// newest first, each shown with a little context (the message right before
// and right after it — or two before, when the mention is the last message
// in the chat) so you don't have to open the chat just to see what it's
// about. Three ways out of a card: open the chat at that exact point, reply
// right here, or dismiss it without replying.
//
// No sidebar list, like Debugging and Meetings — this is one scrollable page.
export function MentionsView({
  nameMap,
  mentionIndex,
  selfDigits,
  onOpenChat,
  onNavigate,
}: {
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  selfDigits?: Set<string>
  /** A mention chip inside a context/mention bubble was clicked — open that
   *  DM, same as everywhere else in the app. */
  onOpenChat: (jid: string) => void
  /** "Open in chat" was clicked on a card — open the chat AND land on this
   *  exact message. */
  onNavigate: (chatJID: string, messageID: string, ts: number) => void
}) {
  const [mentions, setMentions] = useState<Mention[] | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      setMentions(await api.mentions())
      setError('')
    } catch (e) {
      setError(String(e))
    }
  }, [])

  // Thirty seconds: mentions don't need to feel instant, and usePoll stops
  // the moment the window is hidden.
  usePoll(load, 30_000, [load])

  function removeLocally(id: string) {
    setMentions((prev) => (prev ? prev.filter((m) => m.id !== id) : prev))
    // Let the rail badge catch up now instead of waiting for its own poll.
    window.dispatchEvent(new CustomEvent('wa.mentions-changed'))
  }

  if (error && !mentions) {
    return <p className="p-5 text-sm text-red-300">{error}</p>
  }
  if (!mentions) {
    return <p className="p-5 text-sm text-neutral-600">Loading…</p>
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-neutral-950">
      <header className="shrink-0 border-b border-neutral-800 px-5 pt-4 pb-3">
        <h1 className="text-base font-semibold text-neutral-100">Mentions</h1>
        <p className="mt-0.5 text-xs text-neutral-500">
          Every message where you were @-mentioned, newest first.
        </p>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {mentions.length === 0 ? (
          <p className="text-sm text-neutral-600">No mentions right now.</p>
        ) : (
          <div className="mx-auto max-w-2xl space-y-4">
            {mentions.map((m) => (
              <MentionCard
                key={m.chat_jid + ':' + m.id}
                mention={m}
                nameMap={nameMap}
                mentionIndex={mentionIndex}
                selfDigits={selfDigits}
                onOpenChat={onOpenChat}
                onOpen={() => onNavigate(m.chat_jid, m.id, m.timestamp)}
                onDismiss={() => removeLocally(m.id)}
                onReplied={() => removeLocally(m.id)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function MentionCard({
  mention,
  nameMap,
  mentionIndex,
  selfDigits,
  onOpenChat,
  onOpen,
  onDismiss,
  onReplied,
}: {
  mention: Mention
  nameMap: Map<string, string>
  mentionIndex: Map<string, MentionEntry>
  selfDigits?: Set<string>
  onOpenChat: (jid: string) => void
  onOpen: () => void
  onDismiss: () => void
  onReplied: () => void
}) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [dismissing, setDismissing] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [err, setErr] = useState('')

  const cluster = [...(mention.context_before || []), mention, ...(mention.context_after || [])]

  async function send() {
    const body = text.trim()
    if (!body || sending) return
    setSending(true)
    setErr('')
    try {
      await api.reply(mention.chat_jid, mention.id, body)
      // A reply is a form of handling the mention — it drops off the list
      // like a dismiss, without a second "are you sure" step.
      await api.dismissMention(mention.chat_jid, mention.id).catch(() => {})
      onReplied()
    } catch (e) {
      setErr(String(e))
      setSending(false)
    }
  }

  async function dismiss() {
    if (dismissing) return
    setDismissing(true)
    try {
      await api.dismissMention(mention.chat_jid, mention.id)
      onDismiss()
    } catch (e) {
      setErr(String(e))
      setDismissing(false)
    }
  }

  return (
    <div className="overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/40">
      <button
        onClick={onOpen}
        className="flex w-full items-center justify-between gap-3 border-b border-neutral-800 bg-neutral-900/60 px-4 py-2 text-left transition hover:bg-neutral-800/60"
      >
        <span dir="auto" className="truncate text-sm font-medium text-neutral-200">
          {mention.chat_name || mention.chat_jid}
        </span>
        <span className="shrink-0 text-xs text-neutral-500">Open in chat →</span>
      </button>

      <div className="space-y-1 px-3 py-3">
        {cluster.map((msg) => (
          <div key={msg.id} className={msg.id === mention.id ? '' : 'opacity-60'}>
            <MessageBubble
              msg={msg}
              group={msg.is_group}
              nameMap={nameMap}
              mentionIndex={mentionIndex}
              selfDigits={selfDigits}
              onOpenChat={onOpenChat}
              firstInGroup
            />
          </div>
        ))}
      </div>

      <div className="space-y-2 border-t border-neutral-800 bg-neutral-900/60 px-3 py-2.5">
        {err && <p className="text-xs text-red-300">{err}</p>}
        <div className="flex items-end gap-2">
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send()
              }
            }}
            placeholder="Reply…"
            rows={1}
            className="min-w-0 flex-1 resize-none rounded-lg border border-neutral-700 bg-neutral-950 px-2.5 py-1.5 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-neutral-500 focus:outline-none"
          />
          <div className="relative shrink-0">
            <button
              onClick={() => setPickerOpen((v) => !v)}
              title="Quick replies"
              aria-expanded={pickerOpen}
              className={
                'flex h-8 w-8 items-center justify-center rounded-full transition ' +
                (pickerOpen
                  ? 'bg-neutral-800 text-emerald-300'
                  : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200')
              }
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 11.5a8.38 8.38 0 0 1 8.5-8.5 8.5 8.5 0 0 1 8.5 8.5 8.38 8.38 0 0 1-8.5 8.5 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8z" />
                <path d="m8 11 2.5 2.5L15 9" />
              </svg>
            </button>
            {pickerOpen && (
              <QuickReplyPicker
                onPick={(t) => setText((prev) => (prev ? prev + ' ' + t : t))}
                onClose={() => setPickerOpen(false)}
              />
            )}
          </div>
          <button
            onClick={send}
            disabled={sending || !text.trim()}
            className="shrink-0 rounded-lg bg-[#25d366] px-3 py-1.5 text-sm font-medium text-neutral-950 transition disabled:opacity-40"
          >
            {sending ? 'Sending…' : 'Send'}
          </button>
          <button
            onClick={dismiss}
            disabled={dismissing}
            title="Dismiss without replying"
            className="shrink-0 rounded-lg border border-neutral-700 px-2.5 py-1.5 text-xs text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200 disabled:opacity-40"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  )
}

import { Fragment } from 'react'
import { resolveMention, type MentionEntry } from './format'

// Tokenizer for message content: splits text into plain runs, URL hits, and
// "@<digits>" mention hits. Mentions are 7-16 digits to avoid matching things
// like `@123abc` or "@5".
const TOKEN_RE = /(\bhttps?:\/\/[^\s<>"')\]}]+)|(@\d{7,16}\b)/g

// Trailing punctuation we strip from a matched URL so a period at the end of
// a sentence ("see https://x.com.") doesn't get sucked into the link.
const URL_TRAIL = /[.,;:!?)\]}>"']+$/

// RichText renders a message body with:
//   - clickable URLs (opens in new tab)
//   - "@<digits>" mentions resolved to contact names; click → onOpenChat
// Falls back to plain text when neither is needed. dir="auto" keeps mixed
// AR/EN messages aligned correctly.
export function RichText({
  text,
  mentions,
  onOpenChat,
  selfDigits,
  className = '',
}: {
  text: string
  mentions: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
  /** Digit identifiers that map to the current user (phone digits + LID
   *  digits). A mention whose digits match any of these is rendered in
   *  emerald so the user spots being pinged in a busy chat at a glance. */
  selfDigits?: Set<string>
  className?: string
}) {
  if (!text) return null
  const parts = tokenize(text)
  return (
    <div dir="auto" className={'whitespace-pre-wrap break-words text-start ' + className}>
      {parts.map((p, i) => {
        if (p.kind === 'text') return <Fragment key={i}>{p.value}</Fragment>
        if (p.kind === 'url') {
          return (
            <a
              key={i}
              href={p.value}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sky-300 underline decoration-sky-700/60 hover:decoration-sky-300"
              onClick={(e) => e.stopPropagation()}
            >
              {p.display}
            </a>
          )
        }
        // mention
        const r = resolveMention(p.digits, mentions)
        const isSelf = selfDigits?.has(p.digits) ?? false
        return (
          <button
            key={i}
            onClick={(e) => {
              e.stopPropagation()
              onOpenChat?.(r.jid)
            }}
            title={
              isSelf
                ? 'You were mentioned'
                : r.unknown
                  ? 'Unknown contact — open chat anyway'
                  : 'Open DM with ' + r.name
            }
            className={
              'rounded px-1 font-medium ' +
              (isSelf
                ? 'bg-emerald-500/25 text-emerald-200 hover:bg-emerald-500/40'
                : r.unknown
                  ? 'bg-neutral-700/40 text-neutral-300 hover:bg-neutral-700/70'
                  : 'bg-sky-500/15 text-sky-300 hover:bg-sky-500/30')
            }
          >
            @{isSelf ? 'You' : r.unknown ? r.name.replace('+', '') : r.name}
          </button>
        )
      })}
    </div>
  )
}

type Part =
  | { kind: 'text'; value: string }
  | { kind: 'url'; value: string; display: string }
  | { kind: 'mention'; digits: string }

function tokenize(text: string): Part[] {
  const out: Part[] = []
  let last = 0
  TOKEN_RE.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = TOKEN_RE.exec(text)) !== null) {
    if (m.index > last) out.push({ kind: 'text', value: text.slice(last, m.index) })
    if (m[1]) {
      // URL — trim trailing punctuation back into the surrounding text.
      let url = m[1]
      const trail = url.match(URL_TRAIL)
      let punct = ''
      if (trail) {
        punct = trail[0]
        url = url.slice(0, -punct.length)
      }
      out.push({ kind: 'url', value: url, display: url })
      if (punct) out.push({ kind: 'text', value: punct })
    } else if (m[2]) {
      out.push({ kind: 'mention', digits: m[2].slice(1) })
    }
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', value: text.slice(last) })
  return out
}

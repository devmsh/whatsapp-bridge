import { Fragment, type ReactNode } from 'react'
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
        if (p.kind === 'text') return <Fragment key={i}>{formatMarkup(p.value)}</Fragment>
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

// WhatsApp-style inline markup. Each pattern requires non-whitespace right
// after the opening marker and right before the closing marker — matching
// WA's own rule: "*foo *" doesn't bold, "* foo *" doesn't either. Order
// matters because backtick is greedier (no nesting), so we try it first;
// the other three nest recursively (e.g. "*hello _world_*" → bold with
// italic inside).
//
// We deliberately keep this on the plain-text runs only — RichText calls
// it from inside the URL/mention tokenizer, so a URL like
// "https://x.com/_foo_" never gets touched and a mention chip stays a chip.
const MARKUP_PATTERNS: Array<{ tag: 'b' | 'i' | 's' | 'code'; re: RegExp }> = [
  // Inline code first — its content is opaque (no nested formatting), so a
  // `*literal*` inside backticks stays literal.
  { tag: 'code', re: /`([^\n`]+)`/ },
  { tag: 'b', re: /\*([^\s*][^*\n]*?[^\s*]|[^\s*])\*/ },
  { tag: 'i', re: /_([^\s_][^_\n]*?[^\s_]|[^\s_])_/ },
  { tag: 's', re: /~([^\s~][^~\n]*?[^\s~]|[^\s~])~/ },
]

// formatMarkup walks `text` and returns React nodes with bold/italic/strike/
// code applied per WA's rules. Depth-capped so a pathological input can't
// stack thousands of frames; in practice we never see more than a couple
// of nesting levels.
function formatMarkup(text: string, depth = 0): ReactNode {
  if (!text || depth > 8) return text
  // Find the earliest marker in the string (across all four patterns) so we
  // don't pick a later italic when there's a bold sitting in front of it.
  let best: { tag: 'b' | 'i' | 's' | 'code'; match: RegExpExecArray } | null = null
  for (const p of MARKUP_PATTERNS) {
    p.re.lastIndex = 0
    const m = p.re.exec(text)
    if (m && (best === null || m.index < best.match.index)) {
      best = { tag: p.tag, match: m }
    }
  }
  if (!best) return text
  const before = text.slice(0, best.match.index)
  const content = best.match[1]
  const after = text.slice(best.match.index + best.match[0].length)
  // Code blocks don't recurse — their content is literal.
  const inner = best.tag === 'code' ? content : formatMarkup(content, depth + 1)
  return (
    <>
      {before}
      {wrapMarkup(best.tag, inner)}
      {formatMarkup(after, depth + 1)}
    </>
  )
}

function wrapMarkup(tag: 'b' | 'i' | 's' | 'code', inner: ReactNode): ReactNode {
  switch (tag) {
    case 'b':
      return <strong className="font-semibold">{inner}</strong>
    case 'i':
      return <em className="italic">{inner}</em>
    case 's':
      return <span className="line-through">{inner}</span>
    case 'code':
      // Match WA's monospace look — tight padding, soft background.
      return (
        <code className="rounded bg-black/30 px-1 py-0.5 font-mono text-[0.9em]">
          {inner}
        </code>
      )
  }
}

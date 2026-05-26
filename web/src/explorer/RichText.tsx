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
//   - WA-style inline markup (*bold*, _italic_, ~strike~, `code`)
//   - optional substring highlight for in-chat search
// Falls back to plain text when none of those apply. dir="auto" keeps mixed
// AR/EN messages aligned correctly.
export function RichText({
  text,
  mentions,
  onOpenChat,
  selfDigits,
  highlightQuery,
  className = '',
}: {
  text: string
  mentions: Map<string, MentionEntry>
  onOpenChat?: (jid: string) => void
  /** Digit identifiers that map to the current user (phone digits + LID
   *  digits). A mention whose digits match any of these is rendered in
   *  emerald so the user spots being pinged in a busy chat at a glance. */
  selfDigits?: Set<string>
  /** When set, plain-text runs are split around case-insensitive matches
   *  of this string and the matches get an amber `<mark>` background.
   *  Drives the visible yellow highlight on in-chat search hits. */
  highlightQuery?: string
  className?: string
}) {
  if (!text) return null
  // Jumbo-emoji render: messages that are 1-3 emoji (and nothing else)
  // get a much bigger glyph, exactly like WA. Skip all the URL / mention
  // / markup tokenisation — the body is just emoji and any further
  // transform would be wasted work.
  if (isJumboEmoji(text)) {
    return (
      <div dir="auto" className={'whitespace-pre-wrap break-words text-start text-5xl leading-tight ' + className}>
        {text.trim()}
      </div>
    )
  }
  const parts = tokenize(text)
  return (
    <div dir="auto" className={'whitespace-pre-wrap break-words text-start ' + className}>
      {parts.map((p, i) => {
        if (p.kind === 'text') return <Fragment key={i}>{formatMarkup(p.value, 0, highlightQuery)}</Fragment>
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

// isJumboEmoji decides whether a message body should render at the big
// jumbo-emoji size. WA does this when the body is "emoji-only" up to a
// small count (~3 emoji incl. modifiers). We approximate that with two
// gates: cap on the trimmed character length (so a wall of 50 emoji
// doesn't blow up) and a presence check for at least one pictograph,
// plus a reject for any ASCII letter / digit / common punctuation so a
// sentence with a trailing emoji never triggers it.
function isJumboEmoji(text: string): boolean {
  const trimmed = text.trim()
  if (!trimmed || trimmed.length > 12) return false
  if (/[a-zA-Z0-9.,!?;:'"@#$%^&*()_+=\-\[\]{}<>/\\|`~]/.test(trimmed)) return false
  try {
    return /\p{Extended_Pictographic}/u.test(trimmed)
  } catch {
    return false
  }
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
//
// When highlightQuery is set, leaf strings are further split around
// case-insensitive matches and the matches get an amber <mark>. We only
// highlight at the leaves so a query like "foo" still lights up inside
// "*foo*" (the wrapMarkup recursion still threads the query down) but
// never breaks up the markup parsing itself.
function formatMarkup(text: string, depth = 0, highlightQuery?: string): ReactNode {
  if (!text || depth > 8) return highlightString(text, highlightQuery)
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
  if (!best) return highlightString(text, highlightQuery)
  const before = text.slice(0, best.match.index)
  const content = best.match[1]
  const after = text.slice(best.match.index + best.match[0].length)
  // Code blocks don't recurse — their content is literal — but they still
  // get highlight, so a code-fenced match still lights up.
  const inner =
    best.tag === 'code'
      ? highlightString(content, highlightQuery)
      : formatMarkup(content, depth + 1, highlightQuery)
  return (
    <>
      {highlightString(before, highlightQuery)}
      {wrapMarkup(best.tag, inner)}
      {formatMarkup(after, depth + 1, highlightQuery)}
    </>
  )
}

// highlightString splits `text` around case-insensitive occurrences of
// `query` and wraps each match in an amber <mark>. Returns the input
// untouched when no query is set or no match exists — keeps the common
// path zero-overhead.
function highlightString(text: string, query?: string): ReactNode {
  if (!query) return text
  const q = query.trim()
  if (!q) return text
  const lower = text.toLowerCase()
  const needle = q.toLowerCase()
  let i = 0
  let idx = lower.indexOf(needle, i)
  if (idx < 0) return text
  const parts: ReactNode[] = []
  let key = 0
  while (idx >= 0) {
    if (idx > i) parts.push(<Fragment key={key++}>{text.slice(i, idx)}</Fragment>)
    parts.push(
      <mark
        key={key++}
        className="rounded-sm bg-amber-300/40 px-0.5 text-amber-100"
      >
        {text.slice(idx, idx + q.length)}
      </mark>,
    )
    i = idx + q.length
    idx = lower.indexOf(needle, i)
  }
  if (i < text.length) parts.push(<Fragment key={key++}>{text.slice(i)}</Fragment>)
  return <>{parts}</>
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

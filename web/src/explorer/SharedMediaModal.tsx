import { useEffect, useMemo, useState } from 'react'
import type { LightboxImage } from './ImageLightbox'
import type { Message } from '../api'
import { chatListTime, humanSize, mediaURL, senderTitle } from './format'

// SharedMediaModal mirrors WhatsApp's chat-info → "Media, links, docs"
// panel — three tabs over the same haystack of loaded messages:
//
//   Media: every image (LightboxImage list — same one the in-bubble
//          lightbox uses, so the grid can never drift from what the
//          bubbles render).
//   Links: every URL found in any message body / caption — the body
//          extracted on the fly with the same simple URL pattern
//          RichText already understands. Rows show the URL, who sent
//          it, when, and a one-line snippet of the surrounding text.
//   Docs:  every `document` media-type bubble — name, size, sender, when.
//          Click → opens the file in a new tab.
//
// All three tabs read from the messages prop the thread already has, so
// there's no extra fetch and no risk of drift.
export function SharedMediaModal({
  title,
  messages,
  images,
  nameMap,
  onClose,
  onOpenIndex,
}: {
  title: string
  messages: Message[]
  images: LightboxImage[]
  nameMap: Map<string, string>
  onClose: () => void
  onOpenIndex: (index: number) => void
}) {
  const [tab, setTab] = useState<'media' | 'links' | 'docs'>('media')

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  // Extract every URL we can spot in a body or media caption — same simple
  // pattern RichText uses, but here we keep the surrounding text as a
  // snippet so the link row reads as "URL · from John · 2 days ago · ...".
  const links = useMemo(() => extractLinks(messages, nameMap), [messages, nameMap])

  // Documents = bubbles with media_type 'document' AND a downloaded file
  // we can link to. Mirrors what the inline document bubble shows.
  const docs = useMemo(() => extractDocs(messages, nameMap), [messages, nameMap])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[640px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-neutral-100">
              Media, links, docs
            </h2>
            <div dir="auto" className="truncate text-[11px] text-neutral-500">
              {title}
            </div>
          </div>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        {/* Tab strip — same look as WA's three-up segmented control. */}
        <div className="flex shrink-0 border-b border-neutral-800 px-2 pt-2">
          <Tab id="media" current={tab} onPick={setTab} count={images.length}>Media</Tab>
          <Tab id="links" current={tab} onPick={setTab} count={links.length}>Links</Tab>
          <Tab id="docs"  current={tab} onPick={setTab} count={docs.length}>Docs</Tab>
        </div>

        <div className="flex-1 overflow-y-auto p-3">
          {tab === 'media' && (
            images.length === 0 ? (
              <Empty>No photos shared in this chat yet.</Empty>
            ) : (
              <div className="grid grid-cols-4 gap-1.5">
                {images.map((img, i) => (
                  <button
                    key={img.id}
                    onClick={() => onOpenIndex(i)}
                    title={img.caption || ''}
                    className="group relative aspect-square overflow-hidden rounded-md bg-neutral-800 transition hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-emerald-500/50"
                  >
                    <img
                      src={img.url}
                      alt={img.caption || ''}
                      loading="lazy"
                      className="h-full w-full object-cover transition-transform group-hover:scale-105"
                    />
                  </button>
                ))}
              </div>
            )
          )}

          {tab === 'links' && (
            links.length === 0 ? (
              <Empty>No links shared in this chat yet.</Empty>
            ) : (
              <ul className="flex flex-col gap-1">
                {links.map((l, i) => (
                  <li key={l.url + i}>
                    <a
                      href={l.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex flex-col gap-0.5 rounded-md px-3 py-2 transition hover:bg-neutral-800/60"
                    >
                      <span className="truncate text-sm text-sky-300 underline decoration-sky-700/60">
                        {l.url}
                      </span>
                      <span className="flex items-center gap-2 truncate text-[11px] text-neutral-500">
                        <span dir="auto" className="truncate">{l.sender}</span>
                        <span aria-hidden="true">·</span>
                        <span className="shrink-0 tabular-nums">{chatListTime(l.timestamp)}</span>
                        {l.snippet && (
                          <>
                            <span aria-hidden="true">·</span>
                            <span dir="auto" className="truncate text-neutral-400">{l.snippet}</span>
                          </>
                        )}
                      </span>
                    </a>
                  </li>
                ))}
              </ul>
            )
          )}

          {tab === 'docs' && (
            docs.length === 0 ? (
              <Empty>No documents shared in this chat yet.</Empty>
            ) : (
              <ul className="flex flex-col gap-1">
                {docs.map((d) => (
                  <li key={d.id}>
                    <a
                      href={d.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-3 rounded-md px-3 py-2 transition hover:bg-neutral-800/60"
                    >
                      <span aria-hidden="true" className="flex h-9 w-9 shrink-0 items-center justify-center rounded bg-neutral-800 text-base">
                        📄
                      </span>
                      <span className="min-w-0 flex-1">
                        <span dir="auto" className="block truncate text-sm text-neutral-100">
                          {d.name}
                        </span>
                        <span className="flex items-center gap-2 truncate text-[11px] text-neutral-500">
                          <span dir="auto" className="truncate">{d.sender}</span>
                          <span aria-hidden="true">·</span>
                          <span className="shrink-0 tabular-nums">{chatListTime(d.timestamp)}</span>
                          {d.size > 0 && (
                            <>
                              <span aria-hidden="true">·</span>
                              <span className="shrink-0 tabular-nums">{humanSize(d.size)}</span>
                            </>
                          )}
                        </span>
                      </span>
                    </a>
                  </li>
                ))}
              </ul>
            )
          )}
        </div>
      </div>
    </div>
  )
}

function Empty({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-48 items-center justify-center text-xs text-neutral-600">
      {children}
    </div>
  )
}

function Tab({
  id,
  current,
  onPick,
  count,
  children,
}: {
  id: 'media' | 'links' | 'docs'
  current: 'media' | 'links' | 'docs'
  onPick: (id: 'media' | 'links' | 'docs') => void
  count: number
  children: React.ReactNode
}) {
  const active = current === id
  return (
    <button
      onClick={() => onPick(id)}
      className={
        'flex flex-1 items-center justify-center gap-2 border-b-2 px-3 py-2 text-xs font-medium transition ' +
        (active
          ? 'border-emerald-500 text-emerald-300'
          : 'border-transparent text-neutral-400 hover:text-neutral-200')
      }
    >
      <span>{children}</span>
      <span className={'tabular-nums text-[10px] ' + (active ? 'text-emerald-400' : 'text-neutral-500')}>
        {count}
      </span>
    </button>
  )
}

// URL pattern — matches RichText's tokenizer enough to extract every link
// in the chat. Trailing punctuation gets stripped the same way.
const URL_RE = /\bhttps?:\/\/[^\s<>"')\]}]+/g
const URL_TRAIL = /[.,;:!?)\]}>"']+$/

interface LinkRow {
  url: string
  sender: string
  timestamp: number
  snippet: string
}

function extractLinks(messages: Message[], nameMap: Map<string, string>): LinkRow[] {
  const out: LinkRow[] = []
  const seen = new Set<string>()
  // Newest first reads better in a "what's been shared" panel — same
  // ordering WA uses on its own list.
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.is_deleted) continue
    const text = (m.content || '') + (m.media_caption ? ' ' + m.media_caption : '')
    if (!text) continue
    let match: RegExpExecArray | null
    URL_RE.lastIndex = 0
    while ((match = URL_RE.exec(text)) !== null) {
      let url = match[0]
      const trail = url.match(URL_TRAIL)
      if (trail) url = url.slice(0, -trail[0].length)
      // Dedupe — a URL shared three times shows once with the most-recent
      // occurrence's metadata (we're scanning newest first).
      if (seen.has(url)) continue
      seen.add(url)
      out.push({
        url,
        sender: m.is_from_me ? 'You' : senderTitle(m.sender, m.sender_name, m.push_name, nameMap),
        timestamp: m.timestamp,
        // Take ~120 chars around the URL as context, minus the URL itself
        // since it's already the primary line.
        snippet: stripURL(text, url).slice(0, 120).trim(),
      })
    }
  }
  return out
}

function stripURL(text: string, url: string): string {
  return text.replace(url, '').replace(/\s+/g, ' ').trim()
}

interface DocRow {
  id: string
  name: string
  url: string
  sender: string
  timestamp: number
  size: number
}

function extractDocs(messages: Message[], nameMap: Map<string, string>): DocRow[] {
  const out: DocRow[] = []
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.is_deleted) continue
    if (m.media_type !== 'document') continue
    const url = mediaURL(m.media_path, m.chat_jid)
    if (!url) continue
    out.push({
      id: m.id,
      name: m.media_filename || m.media_caption || 'Document',
      url,
      sender: m.is_from_me ? 'You' : senderTitle(m.sender, m.sender_name, m.push_name, nameMap),
      timestamp: m.timestamp,
      size: m.media_size || 0,
    })
  }
  return out
}

import { useEffect, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'

// ClickToChatModal — WhatsApp-Business "click to chat" short link + QR code.
// Builds a wa.me/<number> link (with an optional pre-filled message) for the
// open DM and renders a scannable QR. Pure client-side: the number comes from
// the chat JID, no bridge round-trip. Only meaningful for phone-number JIDs
// (@s.whatsapp.net), so the caller guards groups and @lid chats out.
export function ClickToChatModal({
  phone,
  name,
  onClose,
}: {
  /** Digits only, e.g. "15551234567". */
  phone: string
  name: string
  onClose: () => void
}) {
  const [prefill, setPrefill] = useState('')
  const [copied, setCopied] = useState(false)

  const link =
    'https://wa.me/' + phone + (prefill.trim() ? '?text=' + encodeURIComponent(prefill.trim()) : '')

  // Esc closes — same dismissal contract as the other modals.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard blocked — the link stays selectable in the field */
    }
  }

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex w-full max-w-sm flex-col overflow-hidden rounded-2xl bg-neutral-900 shadow-xl ring-1 ring-neutral-800"
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-neutral-100">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-emerald-300" aria-hidden="true">
              <rect x="3" y="3" width="7" height="7" rx="1" />
              <rect x="14" y="3" width="7" height="7" rx="1" />
              <rect x="3" y="14" width="7" height="7" rx="1" />
              <path d="M14 14h3v3h-3zM21 14v7M17 21h4M21 17h-1" />
            </svg>
            Click to chat
          </h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-500 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex flex-col items-center gap-4 px-5 py-5">
          {/* QR on a light surface so any phone camera scans it cleanly. */}
          <div className="rounded-xl bg-white p-3">
            <QRCodeSVG value={link} size={176} level="M" />
          </div>
          <p className="text-center text-xs text-neutral-400">
            Scan to start a WhatsApp chat with{' '}
            <span className="font-medium text-neutral-200">{name}</span>
          </p>

          {/* Optional pre-filled message — folds straight into the link + QR. */}
          <textarea
            value={prefill}
            onChange={(e) => setPrefill(e.target.value)}
            placeholder="Pre-filled message (optional)…"
            rows={2}
            className="w-full resize-none rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-emerald-500/60 focus:outline-none"
          />

          {/* The link itself — selectable + one-tap copy. */}
          <div className="flex w-full items-center gap-2">
            <input
              readOnly
              value={link}
              onFocus={(e) => e.currentTarget.select()}
              className="min-w-0 flex-1 truncate rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-xs text-neutral-300 focus:outline-none"
            />
            <button
              onClick={copy}
              className="shrink-0 rounded-lg bg-emerald-600 px-3 py-2 text-xs font-medium text-neutral-950 transition hover:bg-emerald-500"
            >
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>

          <a
            href={link}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-emerald-300 transition hover:text-emerald-200"
          >
            Open link ↗
          </a>
        </div>
      </div>
    </div>
  )
}

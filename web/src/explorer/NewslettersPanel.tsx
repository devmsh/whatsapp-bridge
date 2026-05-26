import { useEffect, useState } from 'react'
import { api, type Newsletter } from '../api'
import { ChatAvatar } from './ChatAvatar'

// NewslettersPanel is the modal-form WA "Channels" tab — every channel
// (newsletter) the current user follows, with subscriber count, verified
// badge, role, and per-row mute / unfollow controls.
//
// The bridge already mirrors the metadata table; we just present it and
// let the user manage their subscriptions in one place. Opening a channel
// itself (its message timeline) goes through the standard chat flow — the
// channel's JID is a valid chat JID that ends in @newsletter.
export function NewslettersPanel({
  onClose,
  onOpenChat,
}: {
  onClose: () => void
  onOpenChat: (jid: string) => void
}) {
  const [newsletters, setNewsletters] = useState<Newsletter[] | null>(null)
  const [error, setError] = useState('')
  const [busyJID, setBusyJID] = useState<string | null>(null)

  function refresh() {
    api
      .newsletters()
      .then(setNewsletters)
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load channels'))
  }

  useEffect(() => {
    refresh()
  }, [])

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

  async function toggleMute(n: Newsletter) {
    if (busyJID) return
    setBusyJID(n.jid)
    const next = n.muted !== 'on'
    try {
      await api.newsletterMute(n.jid, next)
      setNewsletters((cur) =>
        (cur || []).map((x) => (x.jid === n.jid ? { ...x, muted: next ? 'on' : 'off' } : x)),
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Mute failed')
    } finally {
      setBusyJID(null)
    }
  }

  async function unfollow(n: Newsletter) {
    if (busyJID) return
    if (
      !window.confirm(
        `Unfollow "${n.name || 'this channel'}"?\n\nYou'll stop receiving its updates and it will disappear from your channels list.`,
      )
    ) return
    setBusyJID(n.jid)
    try {
      await api.newsletterUnfollow(n.jid)
      setNewsletters((cur) => (cur || []).filter((x) => x.jid !== n.jid))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unfollow failed')
    } finally {
      setBusyJID(null)
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[520px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <div className="flex items-baseline gap-2">
            <h2 className="text-sm font-semibold text-neutral-100">Channels</h2>
            {newsletters && (
              <span className="rounded-full bg-neutral-800 px-2 py-0.5 text-[10px] font-medium text-neutral-400">
                {newsletters.length}
              </span>
            )}
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

        <div className="flex-1 overflow-y-auto">
          {error && (
            <div className="m-3 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}
          {!error && newsletters === null && (
            <div className="py-10 text-center text-xs text-neutral-500">Loading channels…</div>
          )}
          {newsletters !== null && newsletters.length === 0 && (
            <div className="py-10 text-center text-xs text-neutral-600">
              You don't follow any channels yet.
            </div>
          )}
          {newsletters?.map((n) => {
            const verified = n.verification_state === 'VERIFIED'
            const isMuted = n.muted === 'on'
            const isAdmin = n.role === 'admin' || n.role === 'owner'
            const isBusy = busyJID === n.jid
            return (
              <div
                key={n.jid}
                className="flex items-start gap-3 border-b border-neutral-900 px-4 py-3"
              >
                <button
                  onClick={() => {
                    onOpenChat(n.jid)
                    onClose()
                  }}
                  className="flex min-w-0 flex-1 items-start gap-3 text-left"
                  title="Open channel"
                >
                  <ChatAvatar jid={n.jid} title={n.name || 'Channel'} size={40} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span dir="auto" className="truncate text-sm font-medium text-neutral-100">
                        {n.name || '(unnamed channel)'}
                      </span>
                      {verified && (
                        <svg
                          viewBox="0 0 24 24"
                          width="12"
                          height="12"
                          fill="currentColor"
                          className="shrink-0 text-emerald-400"
                          aria-label="Verified"
                        >
                          <path d="M12 2 4 5v6c0 4.97 3.582 9.418 8 11 4.418-1.582 8-6.03 8-11V5l-8-3zm-1 14.41-3.41-3.41 1.41-1.41 2 2 5.59-5.58 1.41 1.41-7 7z" />
                        </svg>
                      )}
                      {isAdmin && (
                        <span className="rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[9px] uppercase tracking-wider text-emerald-300">
                          {n.role}
                        </span>
                      )}
                    </div>
                    {n.description && (
                      <div dir="auto" className="line-clamp-2 text-[12px] text-neutral-400">
                        {n.description}
                      </div>
                    )}
                    <div className="text-[11px] text-neutral-500">
                      {n.subscriber_count.toLocaleString()} subscriber{n.subscriber_count === 1 ? '' : 's'}
                    </div>
                  </div>
                </button>
                <div className="flex shrink-0 flex-col items-end gap-1">
                  <button
                    onClick={() => void toggleMute(n)}
                    disabled={isBusy}
                    title={isMuted ? 'Unmute channel' : 'Mute channel'}
                    className={
                      'rounded-md px-2 py-1 text-[10px] uppercase tracking-wider transition disabled:opacity-50 ' +
                      (isMuted
                        ? 'bg-neutral-800 text-neutral-300 hover:bg-neutral-700'
                        : 'text-neutral-500 hover:bg-neutral-800 hover:text-neutral-200')
                    }
                  >
                    {isMuted ? 'Muted' : 'Mute'}
                  </button>
                  <button
                    onClick={() => void unfollow(n)}
                    disabled={isBusy}
                    title="Unfollow this channel"
                    className="rounded-md px-2 py-1 text-[10px] uppercase tracking-wider text-red-300 transition hover:bg-red-950/40 disabled:opacity-50"
                  >
                    Unfollow
                  </button>
                </div>
              </div>
            )
          })}
        </div>

        <footer className="border-t border-neutral-800 px-4 py-2 text-[11px] text-neutral-500">
          Channels are one-way broadcasts. Click any row to read its messages.
        </footer>
      </div>
    </div>
  )
}

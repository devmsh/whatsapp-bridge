import { useEffect, useState } from 'react'
import { api, type DeviceInfo, type LinkedDevice } from '../api'
import { ChatAvatar } from './ChatAvatar'

// SelfProfile is the WA "Settings → Profile" sheet. Shows the user's avatar
// (clickable for the full-size preview, like everywhere else), their push
// name (display name shown to contacts who don't have them saved), the JID
// in phone form, and an editable "About" line — the short bio WA shows
// under your name in profile cards.
//
// Push name: writable on WA via the appstate channel and not exposed by
// whatsmeow as a public method we can call cleanly, so this cycle leaves
// it read-only. The bridge would need a small addition to send the
// PushNameSetting appstate mutation; saved for a follow-up cycle.
//
// About: editable. Bridge GET pulls the current value; PUT pipes through
// SetStatusMessage. Up to ~139 chars (WA's hard cap), enforced on the
// client + by whatsmeow upstream.
export function SelfProfile({
  device,
  onClose,
}: {
  device: DeviceInfo | undefined
  onClose: () => void
}) {
  const [about, setAbout] = useState<string | null>(null) // null while loading
  const [draft, setDraft] = useState('')
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [hint, setHint] = useState('')

  useEffect(() => {
    api
      .selfAbout()
      .then((s) => {
        setAbout(s)
        setDraft(s)
      })
      .catch(() => setAbout(''))
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

  async function save() {
    const next = draft.trim()
    if (next === (about || '').trim()) {
      setEditing(false)
      return
    }
    setSaving(true)
    setError('')
    try {
      await api.setSelfAbout(next)
      setAbout(next)
      setEditing(false)
      setHint('Saved.')
      window.setTimeout(() => setHint(''), 1800)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  // Pretty form of the JID for display: strip the device suffix and the
  // server, prefix '+'. Falls back to '—' if we don't have a device yet
  // (login still completing).
  const jidNonAd = device?.jid?.replace(/:\d+@/, '@')
  const phone = jidNonAd?.split('@')[0] ?? ''
  const displayName = device?.push_name || (phone ? '+' + phone : 'You')

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[88vh] w-[440px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Profile</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        {/* Hero card */}
        {jidNonAd && (
          <div className="flex flex-col items-center gap-2 border-b border-neutral-800 px-4 py-5">
            <ChatAvatar jid={jidNonAd} title={displayName} size={96} clickable />
            <div dir="auto" className="text-base font-semibold text-neutral-100">
              {displayName}
            </div>
            <div className="text-[12px] text-neutral-400">{phone ? '+' + phone : ''}</div>
          </div>
        )}

        <div className="px-4 py-3">
          <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
            About
          </div>
          {about === null ? (
            <div className="py-2 text-xs text-neutral-500">Loading…</div>
          ) : editing ? (
            <div className="flex flex-col gap-2">
              <textarea
                autoFocus
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    setDraft(about)
                    setEditing(false)
                    setError('')
                  }
                }}
                maxLength={139}
                rows={2}
                placeholder="Add a few words about yourself"
                className="w-full resize-none rounded-md border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm text-neutral-200 outline-none focus:border-emerald-500"
              />
              <div className="flex items-center justify-between gap-2">
                <span className="text-[11px] text-neutral-500">
                  {draft.length} / 139
                </span>
                <div className="flex gap-2">
                  <button
                    onClick={() => {
                      setDraft(about)
                      setEditing(false)
                      setError('')
                    }}
                    disabled={saving}
                    className="rounded-md border border-neutral-700 px-2.5 py-1 text-[11px] text-neutral-300 transition hover:bg-neutral-800 disabled:opacity-50"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={save}
                    disabled={saving}
                    className="rounded-md bg-emerald-600 px-2.5 py-1 text-[11px] font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
                  >
                    {saving ? 'Saving…' : 'Save'}
                  </button>
                </div>
              </div>
              {error && <div className="text-[11px] text-red-300">{error}</div>}
            </div>
          ) : (
            <button
              onClick={() => setEditing(true)}
              title="Edit About"
              className="block w-full rounded-md px-3 py-2 text-left text-sm transition hover:bg-neutral-800/50"
            >
              {about ? (
                <span className="text-neutral-200" dir="auto">{about}</span>
              ) : (
                <span className="italic text-neutral-500">Add a few words about yourself</span>
              )}
            </button>
          )}
          {hint && !editing && (
            <div className="mt-1 text-[11px] text-emerald-400">{hint}</div>
          )}
        </div>

        <LinkedDevicesSection />

        <footer className="border-t border-neutral-800 px-4 py-2 text-[11px] text-neutral-500">
          Display name changes still need to happen from the WhatsApp mobile
          app — the bridge can't write the push-name appstate setting yet.
        </footer>
      </div>
    </div>
  )
}

// LinkedDevicesSection lists every device currently signed into this WA
// account — equivalent of WA's Settings → Linked devices. We fetch on mount
// and label the primary phone + the device we're talking to right now so the
// user can spot a session they don't recognise.
//
// We don't currently expose per-device platform / last-seen because
// whatsmeow's user-info usync doesn't return them; the JID's device ID is
// still enough to distinguish rows and gives the user the security signal
// they need (number of devices, whether they recognise them all).
function LinkedDevicesSection() {
  const [devices, setDevices] = useState<LinkedDevice[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .linkedDevices()
      .then((r) => setDevices(r.devices))
      .catch(() => setError('Could not load linked devices.'))
  }, [])

  return (
    <div className="border-t border-neutral-800 px-4 py-3">
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
          Linked devices
        </span>
        {devices && (
          <span className="text-[11px] text-neutral-500">
            {devices.length} {devices.length === 1 ? 'device' : 'devices'}
          </span>
        )}
      </div>
      {error && <div className="text-[11px] text-red-300">{error}</div>}
      {!error && devices === null && (
        <div className="text-[11px] text-neutral-500">Loading…</div>
      )}
      {!error && devices?.length === 0 && (
        <div className="text-[11px] text-neutral-500">No linked devices yet.</div>
      )}
      {devices && devices.length > 0 && (
        <ul className="flex flex-col gap-1">
          {devices.map((d) => {
            // Pull the ":N" suffix for a tiny device-index pill — same way
            // WA mobile shows "Device 3" next to each linked entry.
            const deviceIdx = (() => {
              const m = d.jid.match(/:(\d+)@/)
              return m ? Number(m[1]) : 0
            })()
            return (
              <li
                key={d.jid}
                className="flex items-center gap-3 rounded-md bg-neutral-900/60 px-3 py-2"
              >
                <span
                  aria-hidden="true"
                  className="flex h-7 w-7 items-center justify-center rounded-full bg-neutral-800 text-neutral-300"
                >
                  {d.is_primary ? (
                    /* Phone glyph for primary (the WA phone that scanned QR
                       for every companion). */
                    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <rect x="7" y="2" width="10" height="20" rx="2" />
                      <path d="M11 18h2" />
                    </svg>
                  ) : (
                    /* Laptop glyph for companions */
                    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <rect x="3" y="5" width="18" height="12" rx="2" />
                      <path d="M2 20h20" />
                    </svg>
                  )}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="text-sm text-neutral-100">
                    {d.is_primary ? 'Primary phone' : `Device ${deviceIdx}`}
                  </div>
                  <div className="truncate font-mono text-[10px] text-neutral-500">
                    {d.jid}
                  </div>
                </div>
                {d.is_current && (
                  <span className="shrink-0 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-medium text-emerald-300">
                    This session
                  </span>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

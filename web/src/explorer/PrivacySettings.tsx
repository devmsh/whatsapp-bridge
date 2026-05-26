import { useEffect, useState } from 'react'
import { api, type PrivacySettings as PS } from '../api'

// PrivacySettings is the modal-based equivalent of WA's Settings → Privacy
// panel. Reads /api/v2/privacy on mount and lets the user change any single
// field via the same endpoint (PUT { setting, value }).
//
// Settings layout follows WA exactly:
//
//   Who can see my…
//     Last seen & online
//     Profile photo
//     About
//     Status updates
//   Read receipts
//   Groups (who can add me)
//
// Each row uses a pill picker for its three or four legal values. Saves are
// per-row + optimistic; failures revert and surface the error inline so the
// user knows nothing was persisted. Empty / undefined values from the bridge
// (whatsmeow hasn't synced the field) render as a separate "Default" pip —
// reflects what's actually applied server-side rather than guessing.
export function PrivacySettings({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<PS | null>(null)
  const [error, setError] = useState('')
  // Per-row in-flight setter — disables that row's pills while saving so a
  // mash-click can't fire two writes for the same setting in parallel.
  const [savingKey, setSavingKey] = useState<keyof PS | null>(null)

  useEffect(() => {
    api
      .privacy()
      .then(setData)
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load privacy'))
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

  async function setOne(setting: string, field: keyof PS, value: string) {
    if (!data || savingKey) return
    const prev = data[field]
    if (prev === value) return
    setData({ ...data, [field]: value })
    setSavingKey(field)
    setError('')
    try {
      await api.setPrivacy(setting, value)
    } catch (e) {
      // Revert + surface inline.
      setData({ ...data, [field]: prev })
      setError(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSavingKey(null)
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
        className="flex max-h-[90vh] w-[560px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Privacy</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-4 py-4">
          {error && (
            <div className="mb-3 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
              {error}
            </div>
          )}

          {!data && !error && (
            <div className="py-10 text-center text-xs text-neutral-500">Loading…</div>
          )}

          {data && (
            <>
              <SectionTitle>Who can see my personal info</SectionTitle>
              <Row
                label="Last seen & online"
                hint="When you were last using WhatsApp."
                value={data.LastSeen}
                disabled={savingKey === 'LastSeen'}
                onChange={(v) => setOne('last', 'LastSeen', v)}
                options={WHO_OPTIONS}
              />
              <Row
                label="Profile photo"
                hint="Your avatar in chats and groups."
                value={data.Profile}
                disabled={savingKey === 'Profile'}
                onChange={(v) => setOne('profile', 'Profile', v)}
                options={WHO_OPTIONS}
              />
              <Row
                label="About"
                hint='Your "About" line (the short bio under your name).'
                value={data.Status}
                disabled={savingKey === 'Status'}
                onChange={(v) => setOne('status', 'Status', v)}
                options={WHO_OPTIONS}
              />
              <Row
                label="Status updates"
                hint="Who can view your 24h status updates."
                value={data.Status}
                disabled={savingKey === 'Status'}
                onChange={(v) => setOne('status', 'Status', v)}
                options={WHO_OPTIONS}
              />

              <SectionTitle>Online</SectionTitle>
              <Row
                label="Who can see when I'm online"
                hint='"Same as Last seen" hides your live-online dot unless you also share Last seen.'
                value={data.Online}
                disabled={savingKey === 'Online'}
                onChange={(v) => setOne('online', 'Online', v)}
                options={ONLINE_OPTIONS}
              />

              <SectionTitle>Messages</SectionTitle>
              <Row
                label="Read receipts"
                hint="If turned off, you won't send or receive double-blue ticks. Always on in groups."
                value={data.ReadReceipts}
                disabled={savingKey === 'ReadReceipts'}
                onChange={(v) => setOne('readreceipts', 'ReadReceipts', v)}
                options={READ_RECEIPT_OPTIONS}
              />

              <SectionTitle>Groups & calls</SectionTitle>
              <Row
                label="Who can add me to groups"
                hint='Anyone outside this list has to send you a join request you can accept.'
                value={data.GroupAdd}
                disabled={savingKey === 'GroupAdd'}
                onChange={(v) => setOne('groupadd', 'GroupAdd', v)}
                options={WHO_OPTIONS}
              />
              <Row
                label="Who can call me"
                hint='"Known" silences calls from contacts you have never messaged.'
                value={data.CallAdd}
                disabled={savingKey === 'CallAdd'}
                onChange={(v) => setOne('calladd', 'CallAdd', v)}
                options={CALL_OPTIONS}
              />
            </>
          )}
        </div>

        <footer className="border-t border-neutral-800 px-4 py-2 text-[11px] text-neutral-500">
          Changes apply immediately on WhatsApp's servers. Read receipts also
          control whether you see other people's blue ticks.
        </footer>
      </div>
    </div>
  )
}

// Reusable option sets — the legal values come from whatsmeow's
// types/user.go. Keeping them as named consts makes adding a new row a
// one-liner.
type Opt = { value: string; label: string }
const WHO_OPTIONS: Opt[] = [
  { value: 'all', label: 'Everyone' },
  { value: 'contacts', label: 'My contacts' },
  { value: 'contact_blacklist', label: 'Contacts except…' },
  { value: 'none', label: 'Nobody' },
]
const ONLINE_OPTIONS: Opt[] = [
  { value: 'all', label: 'Everyone' },
  { value: 'match_last_seen', label: 'Same as Last seen' },
]
const READ_RECEIPT_OPTIONS: Opt[] = [
  { value: 'all', label: 'On' },
  { value: 'none', label: 'Off' },
]
const CALL_OPTIONS: Opt[] = [
  { value: 'all', label: 'Everyone' },
  { value: 'known', label: 'Known contacts only' },
]

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mt-3 mb-2 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
      {children}
    </h3>
  )
}

function Row({
  label,
  hint,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string
  hint: string
  value: string
  options: Opt[]
  disabled: boolean
  onChange: (v: string) => void
}) {
  // Empty string = WA hasn't synced the field yet — show the pip set with
  // none of the options active so the user knows the default is in effect.
  return (
    <div className="mb-3 border-b border-neutral-800/60 pb-3 last:border-b-0">
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <span className="text-sm text-neutral-100">{label}</span>
        {value === '' && (
          <span className="text-[10px] uppercase tracking-wider text-neutral-500">
            Default
          </span>
        )}
      </div>
      <div className="mb-1.5 text-[11px] text-neutral-500">{hint}</div>
      <div className="flex flex-wrap gap-1.5">
        {options.map((o) => (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            disabled={disabled}
            className={
              'rounded-full border px-2.5 py-1 text-[11px] font-medium transition disabled:opacity-50 ' +
              (value === o.value
                ? 'border-emerald-500/60 bg-emerald-500/15 text-emerald-200'
                : 'border-neutral-700 bg-neutral-950 text-neutral-300 hover:bg-neutral-800')
            }
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}

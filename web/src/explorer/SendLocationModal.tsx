import { useEffect, useState } from 'react'
import { api } from '../api'

// SendLocationModal — WA's "📍 Location" attachment. We don't ship a real
// in-modal map (would need an external tile provider); the picker is:
//
//   1. Tap "Use my current location" → browser geolocation permission prompt
//      → fills lat/long.
//   2. Or paste / type coordinates manually + an optional "Name" and
//      "Address" (those render as the bubble's title + subtitle on the
//      recipient's side, exactly like WA mobile's saved-place picker).
//   3. Preview line shows the picked coordinates with a Google Maps link
//      so the user can verify before sending.
//
// Static location only — Live Location (the moving "share for 15 min"
// variant) needs a periodic update channel and is saved for a later cycle.
export function SendLocationModal({
  jid,
  onClose,
  onSent,
}: {
  jid: string
  onClose: () => void
  onSent: () => void
}) {
  const [lat, setLat] = useState<string>('')
  const [lng, setLng] = useState<string>('')
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [locating, setLocating] = useState(false)

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

  function useGeo() {
    if (!navigator.geolocation) {
      setError("This browser doesn't expose geolocation. Type the coordinates instead.")
      return
    }
    setLocating(true)
    setError('')
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setLat(pos.coords.latitude.toFixed(6))
        setLng(pos.coords.longitude.toFixed(6))
        setLocating(false)
      },
      (err) => {
        setError(
          err.code === err.PERMISSION_DENIED
            ? 'Location permission denied — type the coordinates manually.'
            : 'Could not get current location: ' + err.message,
        )
        setLocating(false)
      },
      { enableHighAccuracy: true, timeout: 8000 },
    )
  }

  async function send() {
    setError('')
    const latNum = Number(lat)
    const lngNum = Number(lng)
    if (!Number.isFinite(latNum) || !Number.isFinite(lngNum)) {
      setError('Latitude and longitude must be numbers.')
      return
    }
    if (latNum < -90 || latNum > 90) {
      setError('Latitude must be between -90 and 90.')
      return
    }
    if (lngNum < -180 || lngNum > 180) {
      setError('Longitude must be between -180 and 180.')
      return
    }
    setBusy(true)
    try {
      await api.sendLocation(jid, latNum, lngNum, name.trim() || undefined, address.trim() || undefined)
      onSent()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Send failed')
    } finally {
      setBusy(false)
    }
  }

  const havePick = lat !== '' && lng !== ''
  const previewURL = havePick
    ? `https://www.google.com/maps?q=${encodeURIComponent(lat + ',' + lng)}`
    : ''

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[85] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex w-[440px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Send location</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex flex-col gap-3 px-4 py-3">
          <button
            onClick={useGeo}
            disabled={locating || busy}
            className="flex items-center justify-center gap-2 rounded-lg border border-emerald-600/60 bg-emerald-500/10 px-3 py-2 text-sm font-medium text-emerald-200 transition hover:bg-emerald-500/20 disabled:opacity-50"
          >
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="10" r="3" />
              <path d="M12 21s-7-7.58-7-12a7 7 0 0 1 14 0c0 4.42-7 12-7 12z" />
            </svg>
            {locating ? 'Locating…' : 'Use my current location'}
          </button>

          <div className="grid grid-cols-2 gap-2">
            <LabelInput label="Latitude" value={lat} onChange={setLat} placeholder="24.7136" />
            <LabelInput label="Longitude" value={lng} onChange={setLng} placeholder="46.6753" />
          </div>
          <LabelInput label="Name (optional)" value={name} onChange={setName} placeholder="Coffee shop" />
          <LabelInput label="Address (optional)" value={address} onChange={setAddress} placeholder="King Fahd Rd, Riyadh" />

          {havePick && (
            <a
              href={previewURL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-[11px] text-emerald-400 underline transition hover:text-emerald-300"
            >
              Preview on Google Maps ↗
            </a>
          )}
          {error && (
            <div className="rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-[11px] text-red-300">
              {error}
            </div>
          )}
        </div>

        <footer className="flex items-center justify-end gap-2 border-t border-neutral-800 px-4 py-3">
          <button
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-200 transition hover:bg-neutral-800 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={send}
            disabled={busy || !havePick}
            className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
          >
            {busy ? 'Sending…' : 'Send location'}
          </button>
        </footer>
      </div>
    </div>
  )
}

function LabelInput({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        {label}
      </span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="rounded-md border border-neutral-800 bg-neutral-950 px-2.5 py-1.5 text-sm text-neutral-100 outline-none placeholder:text-neutral-600 focus:border-emerald-500"
      />
    </label>
  )
}

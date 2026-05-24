import { useEffect, useState } from 'react'
import { api, type MediaPolicy } from './api'

const TYPES: { key: keyof MediaPolicy; label: string; hint: string }[] = [
  { key: 'images', label: 'Images', hint: 'Photos' },
  { key: 'audio', label: 'Voice notes & audio', hint: 'Voice messages' },
  { key: 'video', label: 'Video', hint: 'Can be large' },
  { key: 'documents', label: 'Documents', hint: 'PDFs, files' },
  { key: 'stickers', label: 'Stickers', hint: '' },
]

// MediaSettings is a modal to control which media auto-downloads. Changes apply
// to newly received messages immediately and persist across restarts.
export function MediaSettings({ onClose }: { onClose: () => void }) {
  const [policy, setPolicy] = useState<MediaPolicy | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    api.mediaSettings().then(setPolicy).catch(() => {})
  }, [])

  function toggle(key: keyof MediaPolicy) {
    if (!policy) return
    setPolicy({ ...policy, [key]: !policy[key] })
    setSaved(false)
  }

  async function save() {
    if (!policy) return
    setSaving(true)
    try {
      const next = await api.setMediaSettings(policy)
      setPolicy(next)
      setSaved(true)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-900 p-6 text-neutral-100"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold">Media download</h2>
          <button onClick={onClose} className="text-neutral-500 hover:text-neutral-300">
            ✕
          </button>
        </div>

        {!policy ? (
          <div className="py-8 text-center text-sm text-neutral-500">Loading…</div>
        ) : (
          <>
            <p className="mb-4 text-xs text-neutral-500">
              Choose what downloads automatically. Skipped media is still recorded — only the file
              is not saved.
            </p>

            <div className="space-y-1">
              {TYPES.map((t) => (
                <label
                  key={t.key}
                  className="flex cursor-pointer items-center justify-between rounded-lg px-3 py-2 hover:bg-neutral-800"
                >
                  <span className="text-sm">
                    {t.label}
                    {t.hint && <span className="ml-2 text-xs text-neutral-500">{t.hint}</span>}
                  </span>
                  <input
                    type="checkbox"
                    checked={!!policy[t.key]}
                    onChange={() => toggle(t.key)}
                    className="h-4 w-4 accent-emerald-500"
                  />
                </label>
              ))}
            </div>

            <div className="mt-4 flex items-center justify-between rounded-lg px-3 py-2">
              <span className="text-sm">
                Max size <span className="text-xs text-neutral-500">(MB, 0 = no limit)</span>
              </span>
              <input
                type="number"
                min={0}
                value={policy.max_size_mb}
                onChange={(e) => {
                  setPolicy({ ...policy, max_size_mb: Math.max(0, Number(e.target.value) || 0) })
                  setSaved(false)
                }}
                className="w-20 rounded-md border border-neutral-700 bg-neutral-950 px-2 py-1 text-right text-sm"
              />
            </div>

            <button
              onClick={save}
              disabled={saving}
              className="mt-5 w-full rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400 disabled:opacity-50"
            >
              {saving ? 'Saving…' : saved ? 'Saved ✓' : 'Save'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}

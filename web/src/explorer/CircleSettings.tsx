import { useState } from 'react'
import { api, type Circle } from '../api'
import { CIRCLE_COLORS } from './colors'

// CircleSettings edits a circle's name, color, notes, and keywords. Keywords are
// saved and then persistently match new groups/contacts — surfaced both in the
// circle's "Suggested" section and in the Recommendations list.
export function CircleSettings({
  circle,
  onClose,
  onChanged,
}: {
  circle: Circle
  onClose: () => void
  onChanged: () => void
}) {
  const [name, setName] = useState(circle.name)
  const [color, setColor] = useState(circle.color || CIRCLE_COLORS[0])
  const [notes, setNotes] = useState(circle.notes || '')
  const [keywords, setKeywords] = useState<string[]>(circle.keywords || [])
  const [kwDraft, setKwDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  function addKeyword() {
    const k = kwDraft.trim()
    if (!k) return
    if (!keywords.some((x) => x.toLowerCase() === k.toLowerCase())) {
      setKeywords([...keywords, k])
      setSaved(false)
    }
    setKwDraft('')
  }

  function removeKeyword(k: string) {
    setKeywords(keywords.filter((x) => x !== k))
    setSaved(false)
  }

  async function save() {
    setSaving(true)
    try {
      await api.updateCircle(circle.id, { name: name.trim() || circle.name, color, notes, keywords })
      setSaved(true)
      onChanged()
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="flex max-h-[85vh] w-full max-w-lg flex-col rounded-2xl border border-neutral-800 bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-5 py-3">
          <h2 className="text-sm font-semibold">Circle settings</h2>
          <button onClick={onClose} className="text-neutral-500 hover:text-neutral-200">
            ✕
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          <label className="mb-1 block text-xs text-neutral-500">Name</label>
          <input
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              setSaved(false)
            }}
            dir="auto"
            className="mb-4 w-full rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-500"
          />

          <label className="mb-1 block text-xs text-neutral-500">Color</label>
          <div className="mb-4 flex flex-wrap gap-2">
            {CIRCLE_COLORS.map((col) => (
              <button
                key={col}
                onClick={() => {
                  setColor(col)
                  setSaved(false)
                }}
                className={
                  'h-6 w-6 rounded-full transition hover:scale-110 ' +
                  (color === col ? 'ring-2 ring-white ring-offset-2 ring-offset-neutral-900' : '')
                }
                style={{ backgroundColor: col }}
              />
            ))}
          </div>

          {/* Keywords */}
          <label className="mb-1 block text-xs text-neutral-500">
            Keywords <span className="text-neutral-600">— auto-suggest matching groups & contacts</span>
          </label>
          <div className="mb-2 flex flex-wrap gap-1.5">
            {keywords.length === 0 && (
              <span className="text-xs text-neutral-600">No keywords yet.</span>
            )}
            {keywords.map((k) => (
              <span
                key={k}
                dir="auto"
                className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-200"
              >
                {k}
                <button onClick={() => removeKeyword(k)} className="text-emerald-300/70 hover:text-red-300">
                  ✕
                </button>
              </span>
            ))}
          </div>
          <div className="mb-4 flex gap-2">
            <input
              value={kwDraft}
              onChange={(e) => setKwDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addKeyword()
                }
              }}
              placeholder="Add a keyword, e.g. Neo Later"
              className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={addKeyword}
              className="rounded-lg border border-neutral-700 px-3 text-sm text-neutral-200 hover:bg-neutral-800"
            >
              Add
            </button>
          </div>

          <label className="mb-1 block text-xs text-neutral-500">Notes (optional)</label>
          <textarea
            value={notes}
            onChange={(e) => {
              setNotes(e.target.value)
              setSaved(false)
            }}
            dir="auto"
            rows={2}
            className="mb-4 w-full resize-none rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-500"
          />

          <button
            onClick={save}
            disabled={saving}
            className="w-full rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400 disabled:opacity-50"
          >
            {saving ? 'Saving…' : saved ? 'Saved ✓' : 'Save'}
          </button>
          <p className="mt-2 text-center text-[11px] text-neutral-600">
            Matches appear in this circle's “Suggested” list and in Recommendations.
          </p>
        </div>
      </div>
    </div>
  )
}

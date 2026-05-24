import { useEffect, useState } from 'react'
import { api, type MemberType, type Recommendation, type RecsResponse } from '../api'

// RecommendationsView shows smart suggestions to build circles, each applyable
// in one click. Hiding a card persists (it won't come back) and the next
// suggestion fills its place; hidden ones can be restored from a small footer.
export function RecommendationsView({
  onChanged,
  onOpenCircle,
}: {
  onChanged: () => void
  onOpenCircle: (id: number) => void
}) {
  const [recs, setRecs] = useState<RecsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [showHidden, setShowHidden] = useState(false)

  function load() {
    setLoading(true)
    api
      .recommendations(6)
      .then((r) => setRecs(r))
      .catch(() => setRecs({ active: [], hidden: [] }))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  async function dismiss(id: string) {
    await api.dismissRecommendation(id).catch(() => {})
    load() // backfill the next suggestion
  }

  async function restore(id: string) {
    await api.restoreRecommendation(id).catch(() => {})
    load()
  }

  const active = recs?.active || []
  const hidden = recs?.hidden || []

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
        <div>
          <div className="text-sm font-semibold">✨ Recommendations</div>
          <div className="text-xs text-neutral-500">Smart ways to build your circles</div>
        </div>
        <button
          onClick={load}
          className="rounded-lg border border-neutral-700 px-3 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
        >
          Refresh
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {loading && <div className="py-10 text-center text-sm text-neutral-600">Analyzing…</div>}
        {!loading && active.length === 0 && (
          <div className="py-10 text-center text-sm text-neutral-600">
            No suggestions right now. Create a few circles and check back.
          </div>
        )}
        <div className="mx-auto flex max-w-2xl flex-col gap-3">
          {active.map((r) => (
            <RecCard
              key={r.id}
              rec={r}
              onDismiss={() => dismiss(r.id)}
              onApplied={(circleId) => {
                load()
                onChanged()
                if (circleId) onOpenCircle(circleId)
              }}
            />
          ))}
        </div>

        {hidden.length > 0 && (
          <div className="mx-auto mt-6 max-w-2xl border-t border-neutral-800 pt-3">
            <button
              onClick={() => setShowHidden((s) => !s)}
              className="text-xs text-neutral-500 hover:text-neutral-300"
            >
              {showHidden ? '▾' : '▸'} {hidden.length} hidden
            </button>
            {showHidden && (
              <div className="mt-2 flex flex-col gap-1">
                {hidden.map((r) => (
                  <div
                    key={r.id}
                    className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-neutral-400 hover:bg-neutral-900"
                  >
                    <span dir="auto" className="min-w-0 flex-1 truncate">
                      {r.name || r.title}
                    </span>
                    <button
                      onClick={() => restore(r.id)}
                      className="shrink-0 rounded-md border border-neutral-700 px-2 py-0.5 text-[11px] text-neutral-300 hover:bg-neutral-800"
                    >
                      Restore
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function RecCard({
  rec,
  onDismiss,
  onApplied,
}: {
  rec: Recommendation
  onDismiss: () => void
  onApplied: (circleId?: number) => void
}) {
  const [name, setName] = useState(rec.name || '')
  const [busy, setBusy] = useState(false)

  const groups = rec.members.filter((m) => m.type === 'group')
  const people = rec.members.filter((m) => m.type === 'contact')

  async function apply() {
    if (busy) return
    setBusy(true)
    try {
      let circleId = rec.circle_id
      if (rec.type === 'new_circle') {
        const created = await api.createCircle(name.trim() || rec.name || 'Circle', rec.color || '#22c55e')
        circleId = created.id
      }
      if (circleId) {
        for (const m of rec.members) {
          await api.addCircleMember(circleId, m.type as MemberType, m.ref).catch(() => {})
        }
      }
      onApplied(circleId)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-xl border border-neutral-800 bg-neutral-900 p-4">
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0">
          {rec.type === 'new_circle' ? (
            <div className="flex items-center gap-2">
              <span
                className="h-3 w-3 shrink-0 rounded-full"
                style={{ backgroundColor: rec.color || '#737373' }}
              />
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                dir="auto"
                className="min-w-0 rounded-md border border-transparent bg-transparent text-sm font-semibold outline-none hover:border-neutral-700 focus:border-neutral-600"
              />
            </div>
          ) : (
            <div dir="auto" className="text-sm font-semibold">
              {rec.title}
            </div>
          )}
          <div className="mt-1 text-xs text-neutral-500">{rec.reason}</div>
        </div>
        <button onClick={onDismiss} className="shrink-0 text-neutral-600 hover:text-neutral-300">
          ✕
        </button>
      </div>

      <div className="mb-3 flex flex-wrap gap-1.5">
        {groups.map((m) => (
          <Chip key={'g' + m.ref} icon="#" label={m.label} tone="sky" />
        ))}
        {people.map((m) => (
          <Chip key={'c' + m.ref} icon="@" label={m.label} tone="neutral" />
        ))}
      </div>

      <button
        onClick={apply}
        disabled={busy}
        className="rounded-lg bg-emerald-500 px-4 py-1.5 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400 disabled:opacity-50"
      >
        {busy
          ? 'Applying…'
          : rec.type === 'new_circle'
            ? `Create circle (${rec.members.length})`
            : `Add ${rec.members.length}`}
      </button>
    </div>
  )
}

function Chip({ icon, label, tone }: { icon: string; label: string; tone: 'sky' | 'neutral' }) {
  return (
    <span
      dir="auto"
      className={
        'inline-flex max-w-[200px] items-center gap-1 truncate rounded-full px-2 py-0.5 text-xs ' +
        (tone === 'sky' ? 'bg-sky-600/20 text-sky-200' : 'bg-neutral-800 text-neutral-300')
      }
    >
      <span className="opacity-60">{icon}</span>
      <span className="truncate">{label}</span>
    </span>
  )
}

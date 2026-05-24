import { useEffect, useRef, useState } from 'react'
import { api, type EntityProfile, type ProfileEntityType } from '../api'

function relTime(unix: number): string {
  if (!unix) return ''
  const s = Math.floor(Date.now() / 1000) - unix
  if (s < 60) return 'just now'
  if (s < 3600) return Math.floor(s / 60) + 'm ago'
  if (s < 86400) return Math.floor(s / 3600) + 'h ago'
  return Math.floor(s / 86400) + 'd ago'
}

// ProfileCard shows an entity's AI-written "purpose" description with the option
// to edit it (pins it as manual) or regenerate it (queues a fresh AI pass and
// polls until it lands). Used in the chat header and circle detail view.
export function ProfileCard({
  type,
  ref_,
  defaultOpen = false,
}: {
  type: ProfileEntityType
  ref_: string
  defaultOpen?: boolean
}) {
  const [profile, setProfile] = useState<EntityProfile | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [open, setOpen] = useState(defaultOpen)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const pollRef = useRef<number | null>(null)

  function load() {
    api
      .getProfile(type, ref_)
      .then((p) => {
        setProfile(p)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }

  useEffect(() => {
    setLoaded(false)
    setEditing(false)
    setProfile(null)
    load()
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type, ref_])

  async function regenerate() {
    if (busy) return
    setBusy(true)
    const before = profile?.updated_at || 0
    try {
      await api.regenerateProfile(type, ref_)
    } catch {
      setBusy(false)
      return
    }
    let tries = 0
    pollRef.current = window.setInterval(async () => {
      tries++
      const p = await api.getProfile(type, ref_).catch(() => null)
      if (p && (p.updated_at > before || p.status === 'ok' || p.status === 'error')) {
        setProfile(p)
        if (p.updated_at > before) {
          setBusy(false)
          if (pollRef.current) window.clearInterval(pollRef.current)
        }
      }
      if (tries > 60) {
        setBusy(false)
        if (pollRef.current) window.clearInterval(pollRef.current)
      }
    }, 3000)
  }

  async function save() {
    setBusy(true)
    try {
      const p = await api.saveProfile(type, ref_, draft)
      setProfile(p)
      setEditing(false)
    } finally {
      setBusy(false)
    }
  }

  if (!loaded) return null

  const desc = profile?.description || ''
  const hasDesc = desc && profile?.status !== 'empty'
  const sourceBadge =
    profile?.source === 'manual' ? 'Edited' : profile?.status === 'error' ? 'Failed' : 'Auto'

  return (
    <div className="border-b border-neutral-800 bg-neutral-900/40 px-4 py-2 text-xs">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 text-left text-neutral-400 hover:text-neutral-200"
      >
        <span className="text-neutral-500">{open ? '▾' : '▸'}</span>
        <span className="font-medium text-neutral-300">Purpose</span>
        {profile && (
          <span className="rounded bg-neutral-800 px-1.5 py-0.5 text-[10px] text-neutral-400">
            {sourceBadge}
          </span>
        )}
        {busy && <span className="text-[10px] text-amber-400">generating…</span>}
        {!open && hasDesc && (
          <span dir="auto" className="min-w-0 flex-1 truncate text-neutral-500">
            {desc}
          </span>
        )}
        {profile?.generated_at ? (
          <span className="ml-auto shrink-0 text-[10px] text-neutral-600">
            {relTime(profile.generated_at)}
          </span>
        ) : null}
      </button>

      {open && (
        <div className="mt-2">
          {editing ? (
            <div>
              <textarea
                dir="auto"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                rows={5}
                className="w-full resize-y rounded-lg border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-xs outline-none focus:border-neutral-500"
                placeholder="Describe what this is about…"
              />
              <div className="mt-1 flex gap-2">
                <button
                  onClick={save}
                  disabled={busy}
                  className="rounded bg-emerald-500/20 px-2 py-1 text-[11px] text-emerald-300 hover:bg-emerald-500/30 disabled:opacity-50"
                >
                  Save
                </button>
                <button
                  onClick={() => setEditing(false)}
                  className="rounded border border-neutral-700 px-2 py-1 text-[11px] text-neutral-300 hover:bg-neutral-800"
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div>
              <p dir="auto" className="whitespace-pre-wrap leading-relaxed text-neutral-300">
                {hasDesc ? desc : <span className="text-neutral-600">No description yet.</span>}
              </p>
              {profile?.status === 'error' && profile.error && (
                <p className="mt-1 text-[11px] text-red-400">Error: {profile.error}</p>
              )}
              <div className="mt-1.5 flex gap-2">
                <button
                  onClick={() => {
                    setDraft(desc === 'No conversation yet.' ? '' : desc)
                    setEditing(true)
                  }}
                  className="rounded border border-neutral-700 px-2 py-1 text-[11px] text-neutral-300 hover:bg-neutral-800"
                >
                  Edit
                </button>
                <button
                  onClick={regenerate}
                  disabled={busy}
                  className="rounded border border-neutral-700 px-2 py-1 text-[11px] text-neutral-300 hover:bg-neutral-800 disabled:opacity-50"
                >
                  {busy ? 'Generating…' : '↻ Regenerate'}
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

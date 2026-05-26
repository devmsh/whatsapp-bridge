import { useEffect, useState } from 'react'
import { api, type GroupFull } from '../api'

// GroupAdminSection — the two binary admin toggles WA exposes on the
// group-info page:
//   - "Announce only"  → only admins can send messages
//   - "Edit group info" locked → only admins can change name/photo/description
//
// Bridge endpoint is PUT /api/v2/groups/{jid}/settings; whatsmeow rejects
// non-admins upstream. We don't gate the UI by admin status — if the user
// taps it without rights, the error surfaces inline. (The dedicated
// admin-check rendering will come once selfDevice flows down into the
// modal — see the future "promote / demote" cycle.)
export function GroupAdminSection({ jid }: { jid: string }) {
  const [group, setGroup] = useState<GroupFull | null>(null)
  const [error, setError] = useState('')
  // Per-toggle in-flight flag so flipping one doesn't block the other and a
  // double-click can't fire twice for the same key.
  const [savingKey, setSavingKey] = useState<'announce' | 'locked' | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .groupGet(jid)
      .then((g) => {
        if (!cancelled) setGroup(g)
      })
      .catch(() => {
        if (!cancelled) setError('Could not load group settings.')
      })
    return () => {
      cancelled = true
    }
  }, [jid])

  async function flip(key: 'announce' | 'locked', next: boolean) {
    if (!group || savingKey) return
    const field = key === 'announce' ? 'is_announce' : 'is_locked'
    const prev = group[field]
    setGroup({ ...group, [field]: next })
    setSavingKey(key)
    setError('')
    try {
      await api.groupSettings(jid, { [key]: next })
    } catch (e) {
      setGroup({ ...group, [field]: prev })
      setError(
        e instanceof Error
          ? e.message + ' — only group admins can change this.'
          : 'Save failed — only group admins can change this.',
      )
    } finally {
      setSavingKey(null)
    }
  }

  if (!group && !error) {
    return (
      <section className="border-b border-neutral-800 px-4 py-3 text-xs text-neutral-600">
        Loading group settings…
      </section>
    )
  }

  return (
    <section className="border-b border-neutral-800 px-4 py-3">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        Group settings
      </div>
      {group && (
        <div className="flex flex-col gap-2">
          <ToggleRow
            label="Announcement group"
            hint="Only admins can send messages."
            checked={!!group.is_announce}
            disabled={savingKey === 'announce'}
            onChange={(v) => flip('announce', v)}
          />
          <ToggleRow
            label="Lock group info"
            hint="Only admins can edit the name, photo, and description."
            checked={!!group.is_locked}
            disabled={savingKey === 'locked'}
            onChange={(v) => flip('locked', v)}
          />
        </div>
      )}
      {error && (
        <div className="mt-2 rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-[11px] text-red-300">
          {error}
        </div>
      )}
    </section>
  )
}

function ToggleRow({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string
  hint: string
  checked: boolean
  disabled: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <label className="flex items-center gap-3">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="h-4 w-4 accent-emerald-500 disabled:opacity-50"
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm text-neutral-100">{label}</div>
        <div className="text-[11px] text-neutral-500">{hint}</div>
      </div>
    </label>
  )
}

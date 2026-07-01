import { useEffect, useState } from 'react'
import { api, type Circle, type CircleMember } from '../api'
import { initial, jidUser } from './format'
import { ProfileCard } from './ProfileCard'

// FocusProfile is Focus Mode's profile panel: the circle's AI-written purpose
// (reusing ProfileCard, same as CircleView.tsx) plus a read-only member list.
// No add/remove/expand controls (those stay in CircleView.tsx) and no tags
// section (circles don't support the tag-chip system contacts/groups use).
export function FocusProfile({
  circleId,
  circles,
  nameMap,
}: {
  circleId: number
  circles: Circle[]
  nameMap: Map<string, string>
}) {
  const [members, setMembers] = useState<CircleMember[]>([])

  useEffect(() => {
    let cancelled = false
    api
      .getCircle(circleId)
      .then((detail) => {
        if (!cancelled) setMembers(detail.members || [])
      })
      .catch(() => {
        if (!cancelled) setMembers([])
      })
    return () => {
      cancelled = true
    }
  }, [circleId])

  function memberLabel(m: CircleMember): string {
    if (m.member_type === 'circle') {
      return circles.find((c) => String(c.id) === m.member_ref)?.name || `Circle ${m.member_ref}`
    }
    return nameMap.get(m.member_ref) || '+' + jidUser(m.member_ref)
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <ProfileCard type="circle" ref_={String(circleId)} defaultOpen />

      <div className="min-h-0 flex-1 p-4">
        <div className="mb-2 text-[11px] uppercase tracking-wide text-neutral-500">
          Members · {members.length}
        </div>
        {members.length === 0 && (
          <div className="py-4 text-center text-sm text-neutral-600">Empty circle.</div>
        )}
        <div className="space-y-1">
          {members.map((m) => {
            const label = memberLabel(m)
            return (
              <div
                key={m.member_type + ':' + m.member_ref}
                className="flex items-center gap-3 rounded-lg px-2 py-2"
              >
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-neutral-700 text-sm font-semibold text-neutral-200">
                  {initial(label)}
                </span>
                <span className="min-w-0 flex-1">
                  <span dir="auto" className="block truncate text-sm">
                    {label}
                  </span>
                  <span className="block truncate text-xs text-neutral-500">
                    {m.member_type === 'circle' ? 'Circle' : m.member_type === 'group' ? 'Group' : 'Contact'}
                  </span>
                </span>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

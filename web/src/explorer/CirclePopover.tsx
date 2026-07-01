import type { Circle } from '../api'
import { FocusProfile } from './FocusProfile'

// CirclePopover is a thin wrapper that renders the existing FocusProfile
// (purpose via ProfileCard + read-only member list) inside a small anchored
// popover instead of a permanent stacked panel. Triggered by tapping the
// circle name in the header; dismissible via click-away or Escape-adjacent
// close handlers wired by the parent. Mirrors the click-away + anchoring
// idiom already used by FocusSwitcher.tsx.
export function CirclePopover({
  open,
  onClose,
  circleId,
  circles,
  nameMap,
}: {
  open: boolean
  onClose: () => void
  circleId: number
  circles: Circle[]
  nameMap: Map<string, string>
}) {
  if (!open) return null

  return (
    <>
      {/* Click-away catcher dismisses on any outside click. */}
      <div onClick={onClose} className="fixed inset-0 z-30" aria-hidden="true" />
      <div
        onClick={(e) => e.stopPropagation()}
        className="absolute left-0 top-full z-40 mt-1 max-h-[70vh] w-96 overflow-y-auto rounded-lg border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60 p-3"
      >
        <FocusProfile circleId={circleId} circles={circles} nameMap={nameMap} />
      </div>
    </>
  )
}

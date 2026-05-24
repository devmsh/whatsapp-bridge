import { useEffect, useState } from 'react'
import { api } from '../api'
import { getUnlockToken, setUnlockToken } from '../hidden'

// HiddenBadge appears in the Explorer top bar when hidden chats exist. If
// locked, it's a 🔒 icon that opens the unlock modal on click. If unlocked,
// it shows "🔓 Visible" with a re-lock action. It also listens for the global
// wa.unlock-changed event so it reflects state changes from other components.
export function HiddenBadge({ onClick }: { onClick: () => void }) {
  const [unlocked, setUnlocked] = useState<boolean>(!!getUnlockToken())
  const [hasHidden, setHasHidden] = useState<boolean>(false)

  useEffect(() => {
    function sync() {
      setUnlocked(!!getUnlockToken())
      api
        .hiddenStatus()
        .then((st) => setHasHidden(st.hidden_count > 0))
        .catch(() => {})
    }
    sync()
    window.addEventListener('wa.unlock-changed', sync)
    return () => window.removeEventListener('wa.unlock-changed', sync)
  }, [])

  async function relock() {
    await api.hiddenLock().catch(() => {})
    setUnlockToken(null)
  }

  if (!hasHidden && !unlocked) return null

  if (unlocked) {
    return (
      <button
        onClick={relock}
        title="Hidden chats are visible — click to lock"
        className="flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/20 px-2 py-1 text-[11px] font-medium text-amber-300 hover:bg-amber-500/30"
      >
        🔓 Visible
      </button>
    )
  }
  return (
    <button
      onClick={onClick}
      title="Unlock hidden chats"
      className="flex shrink-0 items-center gap-1 rounded-lg border border-neutral-700 px-2 py-1 text-[11px] text-neutral-400 hover:bg-neutral-800"
    >
      🔒
    </button>
  )
}

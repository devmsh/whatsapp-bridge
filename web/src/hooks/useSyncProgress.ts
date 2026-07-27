import { useEffect, useState } from 'react'
import { api, type SyncStatus } from '../api'
import { isAppVisible } from './usePoll'

// useSyncProgress polls /api/v2/sync/progress while enabled. Polling is fine
// here: the values change a few times per second at most during a sync.
export function useSyncProgress(enabled: boolean, intervalMs = 1000): SyncStatus | null {
  const [status, setStatus] = useState<SyncStatus | null>(null)

  useEffect(() => {
    if (!enabled) return
    let stop = false

    async function tick() {
      try {
        const s = await api.syncProgress()
        if (!stop) setStatus(s)
      } catch {
        // transient — try again next tick
      }
    }
    // Visibility-gated: a hidden macOS window must not keep polling.
    let id: number | undefined
    const start = () => {
      if (id !== undefined) return
      tick()
      id = window.setInterval(tick, intervalMs)
    }
    const halt = () => {
      if (id === undefined) return
      window.clearInterval(id)
      id = undefined
    }
    const onVis = () => (isAppVisible() ? start() : halt())
    onVis()
    document.addEventListener('visibilitychange', onVis)
    window.addEventListener('wa-visibility', onVis)
    return () => {
      stop = true
      halt()
      document.removeEventListener('visibilitychange', onVis)
      window.removeEventListener('wa-visibility', onVis)
    }
  }, [enabled, intervalMs])

  return status
}

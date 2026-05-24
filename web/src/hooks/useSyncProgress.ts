import { useEffect, useState } from 'react'
import { api, type SyncStatus } from '../api'

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
    tick()
    const id = setInterval(tick, intervalMs)
    return () => {
      stop = true
      clearInterval(id)
    }
  }, [enabled, intervalMs])

  return status
}

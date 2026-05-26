import { useEffect, useState } from 'react'
import { api } from '../api'

// useBlocklist gives every consumer the current set of blocked JIDs and
// refetches whenever something dispatches `wa.blocklist-changed` — the
// pattern ContactInfoModal uses after a successful block/unblock so the
// chat header / composer can react without each surface polling on its own.
//
// One in-flight fetch per mount; the bridge call is cheap (~ms) so we don't
// bother with a global store. Membership checks are O(1) on a Set.
//
// Returns the set itself (use .has(jid)) plus a manual refresh — handy when
// a mutation happens outside the standard event flow.
export function useBlocklist(): { blocked: Set<string>; refresh: () => void } {
  const [blocked, setBlocked] = useState<Set<string>>(() => new Set())

  function refresh() {
    api.blocklist().then((list) => setBlocked(new Set(list))).catch(() => {})
  }

  useEffect(() => {
    refresh()
    window.addEventListener('wa.blocklist-changed', refresh)
    return () => window.removeEventListener('wa.blocklist-changed', refresh)
  }, [])

  return { blocked, refresh }
}

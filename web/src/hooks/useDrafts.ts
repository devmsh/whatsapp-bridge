import { useEffect, useState } from 'react'

// useDrafts is a small subscriber that keeps a live snapshot of every saved
// per-chat draft (the localStorage entries the Composer writes on every
// keystroke — see web/src/explorer/MessageThread.tsx → draftKey).
//
// Hands you back a Map<jid, draftText> so the ChatList can render WA's
// "Draft: …" indicator in red without each row reaching into localStorage
// on every render. Re-reads when:
//   - the window's `storage` event fires (another tab changed a draft)
//   - the Composer dispatches `wa.draft-changed` (same-tab change — storage
//     events do not fire in the same tab that wrote the key)
//
// Empty / cleared entries are skipped so a key that lingers as "" doesn't
// trick the list into showing a phantom draft.
const DRAFT_PREFIX = 'wa.draft.'
export const DRAFT_CHANGED_EVENT = 'wa.draft-changed'

export function useDrafts(): Map<string, string> {
  const [drafts, setDrafts] = useState<Map<string, string>>(() => readAllDrafts())

  useEffect(() => {
    function refresh() {
      setDrafts(readAllDrafts())
    }
    window.addEventListener('storage', refresh)
    window.addEventListener(DRAFT_CHANGED_EVENT, refresh)
    return () => {
      window.removeEventListener('storage', refresh)
      window.removeEventListener(DRAFT_CHANGED_EVENT, refresh)
    }
  }, [])

  return drafts
}

function readAllDrafts(): Map<string, string> {
  const out = new Map<string, string>()
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (!k || !k.startsWith(DRAFT_PREFIX)) continue
      const v = localStorage.getItem(k)
      if (!v) continue
      out.set(k.slice(DRAFT_PREFIX.length), v)
    }
  } catch {}
  return out
}

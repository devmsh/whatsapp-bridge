import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { decodeRequestOptions, encodeCredential, setChatUnlock } from '../hidden'

// ChatUnlockModal asks for a fingerprint to open ONE hidden chat. No PIN
// step — the backend chat-options endpoint mints a chat-scoped challenge
// directly. On success it stores the per-chat token in `hidden.ts`'s
// chatTokens map (in-memory) and calls onUnlocked(jid).
//
// The whole UI stays in normal mode; only this one chat becomes openable.
export function ChatUnlockModal({
  chatJID,
  contactName,
  onClose,
  onUnlocked,
}: {
  chatJID: string
  contactName: string
  onClose: () => void
  onUnlocked: (jid: string) => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const started = useRef(false)

  async function tryUnlock() {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const { publicKey, session_id } = await api.hiddenChatOptions(chatJID)
      const opts = decodeRequestOptions(publicKey)
      const credRaw = (await navigator.credentials.get({
        publicKey: opts,
      })) as PublicKeyCredential | null
      if (!credRaw) throw new Error('Touch ID was cancelled')
      const cred = encodeCredential(credRaw)
      const { unlock_token, ttl_seconds } = await api.hiddenChatVerify(session_id, cred)
      setChatUnlock(chatJID, unlock_token, ttl_seconds)
      onUnlocked(chatJID)
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setBusy(false)
    }
  }

  // Auto-trigger the Touch ID prompt on mount so the user goes straight from
  // "click mention" to the OS fingerprint dialog. Falls back to a manual
  // button if the auto-prompt fails or is dismissed.
  useEffect(() => {
    if (started.current) return
    started.current = true
    void tryUnlock()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatJID])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-sm rounded-2xl border border-neutral-800 bg-neutral-950 p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center gap-2 text-2xl">🔒</div>
        <div className="mb-1 text-sm font-semibold">Locked chat</div>
        <div className="mb-4 text-sm text-neutral-400">
          Approve with Touch ID to open the chat with{' '}
          <span className="font-medium text-neutral-200">{contactName}</span>.
        </div>
        {error && (
          <div className="mb-3 rounded-lg border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-lg px-3 py-1.5 text-sm text-neutral-400 hover:text-neutral-200"
          >
            Cancel
          </button>
          <button
            onClick={tryUnlock}
            disabled={busy}
            className="rounded-lg bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
          >
            {busy ? 'Waiting for Touch ID…' : 'Try again'}
          </button>
        </div>
      </div>
    </div>
  )
}

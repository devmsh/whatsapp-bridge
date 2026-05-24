import { useEffect, useRef, useState } from 'react'
import { api, type HiddenStatus } from '../api'
import {
  setUnlockToken,
  webauthnAssert,
  webauthnRegister,
} from '../hidden'

// Phase shared across setup and unlock — the modal does one of these flows.
type Mode = 'setup' | 'unlock'

// HiddenLockModal handles both first-time SETUP (set PIN + register Touch ID)
// and UNLOCK (verify PIN + Touch ID assertion -> store unlock token).
// On success, `onUnlocked` is called.
export function HiddenLockModal({
  initialMode,
  prefilledPin,
  onUnlocked,
  onClose,
}: {
  initialMode?: Mode
  prefilledPin?: string
  onUnlocked: () => void
  onClose: () => void
}) {
  const [status, setStatus] = useState<HiddenStatus | null>(null)
  const [mode, setMode] = useState<Mode>(initialMode || 'unlock')
  const [pin, setPin] = useState(prefilledPin || '')
  const [pin2, setPin2] = useState('')
  const [busy, setBusy] = useState(false)
  const [step, setStep] = useState<'pin' | 'biometric' | 'done'>('pin')
  const [error, setError] = useState<string | null>(null)
  const [pinPassed, setPinPassed] = useState<string | null>(null)

  // Load status to pick the right default mode.
  useEffect(() => {
    api.hiddenStatus().then((st) => {
      setStatus(st)
      if (!initialMode) setMode(st.pin_set ? 'unlock' : 'setup')
    })
  }, [initialMode])

  // If we were given a prefilled PIN (search-bar typing flow), auto-submit.
  const autoTried = useRef(false)
  useEffect(() => {
    if (
      !autoTried.current &&
      mode === 'unlock' &&
      prefilledPin &&
      pin === prefilledPin &&
      step === 'pin'
    ) {
      autoTried.current = true
      void submitPin()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, prefilledPin, step])

  async function submitSetupPin() {
    if (pin.length < 4) {
      setError('PIN must be at least 4 characters')
      return
    }
    if (pin !== pin2) {
      setError("PINs don't match")
      return
    }
    setError(null)
    setBusy(true)
    try {
      await api.hiddenPinSetup(pin)
      // Now flip to unlock flow so we can register Touch ID under a valid
      // pin-passed token. Verify the PIN we just set.
      const r = await api.hiddenUnlockPin(pin)
      setPinPassed(r.pin_passed_token)
      // Register Touch ID.
      const optsRes = await fetch('/api/v2/hidden/webauthn/register/options', {
        method: 'POST',
        headers: { 'X-Pin-Passed': r.pin_passed_token },
      })
      if (!optsRes.ok) throw new Error('Register options failed: ' + optsRes.status)
      const { publicKey, session_id } = await optsRes.json()
      const cred = await webauthnRegister(publicKey)
      const verifyRes = await fetch('/api/v2/hidden/webauthn/register/verify', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Pin-Passed': r.pin_passed_token,
        },
        body: JSON.stringify({ session_id, credential: cred }),
      })
      if (!verifyRes.ok) {
        const err = await verifyRes.json().catch(() => ({}))
        throw new Error('Register verify: ' + (err.error || verifyRes.status))
      }
      // Right after register, do an assertion to mint the unlock token so the
      // user is unlocked at the end of setup.
      const r2 = await api.hiddenUnlockPin(pin)
      await assertAndUnlock(r2.pin_passed_token)
      onUnlocked()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function submitPin() {
    if (pin.length < 4) {
      setError('Enter your PIN')
      return
    }
    setError(null)
    setBusy(true)
    try {
      const r = await api.hiddenUnlockPin(pin)
      setPinPassed(r.pin_passed_token)
      if (!r.webauthn_registered) {
        // Touch ID not yet registered — register it now.
        setStep('biometric')
        const optsRes = await fetch('/api/v2/hidden/webauthn/register/options', {
          method: 'POST',
          headers: { 'X-Pin-Passed': r.pin_passed_token },
        })
        if (!optsRes.ok) throw new Error('Register options failed: ' + optsRes.status)
        const { publicKey, session_id } = await optsRes.json()
        const cred = await webauthnRegister(publicKey)
        const verifyRes = await fetch('/api/v2/hidden/webauthn/register/verify', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Pin-Passed': r.pin_passed_token,
          },
          body: JSON.stringify({ session_id, credential: cred }),
        })
        if (!verifyRes.ok) {
          const err = await verifyRes.json().catch(() => ({}))
          throw new Error('Register verify: ' + (err.error || verifyRes.status))
        }
        // Now re-do PIN to mint a fresh pin-passed and then assert.
        const r2 = await api.hiddenUnlockPin(pin)
        await assertAndUnlock(r2.pin_passed_token)
      } else {
        setStep('biometric')
        await assertAndUnlock(r.pin_passed_token)
      }
      onUnlocked()
    } catch (e) {
      setError((e as Error).message)
      setStep('pin')
    } finally {
      setBusy(false)
    }
  }

  async function assertAndUnlock(pinPassedToken: string) {
    const optsRes = await fetch('/api/v2/hidden/webauthn/auth/options', {
      method: 'POST',
      headers: { 'X-Pin-Passed': pinPassedToken },
    })
    if (!optsRes.ok) throw new Error('Auth options failed: ' + optsRes.status)
    const { publicKey, session_id } = await optsRes.json()
    const cred = await webauthnAssert(publicKey)
    const verifyRes = await fetch('/api/v2/hidden/webauthn/auth/verify', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Pin-Passed': pinPassedToken,
      },
      body: JSON.stringify({ session_id, credential: cred }),
    })
    if (!verifyRes.ok) {
      const err = await verifyRes.json().catch(() => ({}))
      throw new Error('Auth verify: ' + (err.error || verifyRes.status))
    }
    const { unlock_token } = await verifyRes.json()
    setUnlockToken(unlock_token)
  }

  const title =
    mode === 'setup'
      ? 'Set up locked chats'
      : step === 'biometric'
        ? 'Approve with Touch ID'
        : 'Unlock locked chats'

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-950 p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-2 flex items-center justify-between">
          <div className="text-sm font-semibold">{title}</div>
          <button
            onClick={onClose}
            className="rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          >
            Close
          </button>
        </div>

        {mode === 'setup' && (
          <div className="space-y-3">
            <p className="text-xs text-neutral-400">
              Pick a PIN (4–12 characters). After this, Touch ID will be registered. Both will be
              required to view or hide chats.
            </p>
            <input
              autoFocus
              type="password"
              inputMode="numeric"
              value={pin}
              onChange={(e) => setPin(e.target.value)}
              placeholder="New PIN"
              className="w-full rounded-lg border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <input
              type="password"
              inputMode="numeric"
              value={pin2}
              onChange={(e) => setPin2(e.target.value)}
              placeholder="Confirm PIN"
              className="w-full rounded-lg border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={submitSetupPin}
              disabled={busy}
              className="w-full rounded-lg bg-emerald-500 px-3 py-2 text-sm font-semibold text-neutral-950 hover:bg-emerald-400 disabled:opacity-50"
            >
              {busy ? 'Working…' : 'Set PIN + register Touch ID'}
            </button>
          </div>
        )}

        {mode === 'unlock' && step === 'pin' && (
          <div className="space-y-3">
            <p className="text-xs text-neutral-400">
              Enter your PIN. You'll be asked for Touch ID right after.
            </p>
            <input
              autoFocus
              type="password"
              inputMode="numeric"
              value={pin}
              onChange={(e) => setPin(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') submitPin()
              }}
              placeholder="PIN"
              className="w-full rounded-lg border border-neutral-700 bg-neutral-900 px-3 py-2 text-sm outline-none focus:border-neutral-500"
            />
            <button
              onClick={submitPin}
              disabled={busy}
              className="w-full rounded-lg bg-emerald-500 px-3 py-2 text-sm font-semibold text-neutral-950 hover:bg-emerald-400 disabled:opacity-50"
            >
              {busy ? 'Checking…' : 'Continue'}
            </button>
          </div>
        )}

        {mode === 'unlock' && step === 'biometric' && (
          <div className="space-y-3 py-4 text-center">
            <div className="text-2xl">👆</div>
            <p className="text-sm text-neutral-300">Approve with Touch ID to unlock.</p>
            {pinPassed && (
              <button
                onClick={() => assertAndUnlock(pinPassed).then(onUnlocked).catch((e) => setError((e as Error).message))}
                className="rounded-lg border border-neutral-700 px-3 py-1.5 text-xs text-neutral-300 hover:bg-neutral-800"
              >
                Try again
              </button>
            )}
          </div>
        )}

        {error && <div className="mt-3 text-xs text-red-400">{error}</div>}

        {status && mode === 'unlock' && (
          <p className="mt-3 text-[11px] text-neutral-600">
            {status.hidden_count} chat{status.hidden_count === 1 ? '' : 's'} currently hidden.
          </p>
        )}
      </div>
    </div>
  )
}

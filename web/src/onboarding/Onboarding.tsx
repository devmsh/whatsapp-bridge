import { useState } from 'react'
import { api, type AuthState } from '../api'
import { Card, Spinner } from './Card'
import { QRScreen } from './QRScreen'

// Onboarding renders the right screen for every non-connected auth state.
export function Onboarding({ auth }: { auth: AuthState }) {
  if (auth.state === 'qr' && auth.qr_code) {
    return <QRScreen qrCode={auth.qr_code} />
  }
  if (auth.state === 'error') {
    return <ErrorScreen message={auth.error || 'Something went wrong'} />
  }
  if (auth.state === 'logged_out') {
    return <LoggedOutScreen />
  }
  // connecting | pairing | qr-without-code-yet
  const label = auth.state === 'pairing' ? 'Pairing your phone…' : 'Connecting to WhatsApp…'
  return (
    <Card>
      <div className="flex items-center gap-3 py-6">
        <Spinner />
        <span className="text-sm text-neutral-300">{label}</span>
      </div>
    </Card>
  )
}

function ErrorScreen({ message }: { message: string }) {
  const [busy, setBusy] = useState(false)
  return (
    <Card>
      <h2 className="mb-1 text-base font-medium text-red-400">Login failed</h2>
      <p className="mb-6 text-sm text-neutral-400">{message}</p>
      <RetryButton busy={busy} setBusy={setBusy} />
    </Card>
  )
}

function LoggedOutScreen() {
  const [busy, setBusy] = useState(false)
  return (
    <Card>
      <h2 className="mb-1 text-base font-medium">Not linked</h2>
      <p className="mb-6 text-sm text-neutral-400">
        This bridge is not linked to a WhatsApp account yet.
      </p>
      <RetryButton busy={busy} setBusy={setBusy} label="Start login" />
    </Card>
  )
}

function RetryButton({
  busy,
  setBusy,
  label = 'Try again',
}: {
  busy: boolean
  setBusy: (b: boolean) => void
  label?: string
}) {
  return (
    <button
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        try {
          await api.login()
        } finally {
          setBusy(false)
        }
      }}
      className="w-full rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400 disabled:opacity-50"
    >
      {busy ? 'Working…' : label}
    </button>
  )
}

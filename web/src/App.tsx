import { useEffect, useState } from 'react'
import { useAuth } from './hooks/useAuth'
import { Onboarding } from './onboarding/Onboarding'
import { SyncScreen } from './onboarding/SyncScreen'
import { Explorer } from './explorer/Explorer'

export default function App() {
  const auth = useAuth()

  // sawLink is true once we've seen a not-yet-linked state (QR/pairing/logged
  // out) during this page load. That tells apart "just linked now" from
  // "already linked when the page opened" — so a refresh of a linked session
  // goes straight to the explorer instead of the welcome/sync screen.
  const [sawLink, setSawLink] = useState(false)
  const [enteredApp, setEnteredApp] = useState(false)

  useEffect(() => {
    if (auth.state === 'qr' || auth.state === 'pairing' || auth.state === 'logged_out') {
      setSawLink(true)
    }
  }, [auth.state])

  if (auth.state !== 'connected') {
    return <Onboarding auth={auth} />
  }

  // Show the sync/welcome screen only right after a fresh link.
  if (sawLink && !enteredApp) {
    return <SyncScreen device={auth.device} onContinue={() => setEnteredApp(true)} />
  }

  return <Explorer device={auth.device} />
}

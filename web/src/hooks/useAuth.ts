import { useEffect, useRef, useState } from 'react'
import { api, type AuthState } from '../api'

// useAuth subscribes to the live login state via SSE (/api/v2/auth/stream).
// It falls back to a one-shot fetch if the stream cannot open, and reconnects
// automatically if the connection drops.
export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>({
    state: 'connecting',
    updated_at: 0,
  })
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    let closed = false

    // Seed with a one-shot fetch so we render fast even before SSE delivers.
    api.authStatus().then((s) => {
      if (!closed) setState(s)
    }).catch(() => {})

    function connect() {
      if (closed) return
      const es = new EventSource('/api/v2/auth/stream')
      esRef.current = es
      es.onmessage = (e) => {
        try {
          setState(JSON.parse(e.data) as AuthState)
        } catch {
          // ignore malformed frames
        }
      }
      es.onerror = () => {
        es.close()
        if (!closed) setTimeout(connect, 1500)
      }
    }
    connect()

    return () => {
      closed = true
      esRef.current?.close()
    }
  }, [])

  return state
}

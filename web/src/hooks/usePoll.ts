import { useEffect, useRef } from 'react'

// Is the app actually on screen?
//
// In a browser tab, document.visibilityState is the answer. Inside the macOS
// shell it is NOT sufficient: WebKit does not reliably report a window that
// has been ordered out as hidden, so the shell sets __WA_APP_VISIBLE__
// explicitly (see macapp/Sources/MainWindowController.swift). When that flag
// is present it wins, because it is the only one that knows about a hidden
// or miniaturised window.
export function isAppVisible(): boolean {
  if (typeof window === 'undefined') return false
  const explicit = (window as { __WA_APP_VISIBLE__?: boolean }).__WA_APP_VISIBLE__
  if (typeof explicit === 'boolean') return explicit
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

// usePoll runs `fn` on an interval, but ONLY while the page is visible.
//
// This matters a lot in the macOS app (macapp/): closing the window hides it
// rather than quitting, so an unguarded setInterval keeps waking the CPU for
// the rest of the day. WebKit reports visibilityState 'hidden' for a window
// that has been ordered out, so gating on it suspends every poller the moment
// the user closes the window, and resumes with an immediate tick when they
// come back — no stale first paint.
//
// `fn` is held in a ref so a caller passing an inline arrow does not restart
// the interval on every render.
export function usePoll(
  fn: () => void | Promise<void>,
  intervalMs: number,
  deps: unknown[] = [],
) {
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined

    const run = () => {
      if (cancelled) return
      void fnRef.current()
    }

    const start = () => {
      if (timer !== undefined || cancelled) return
      run() // tick immediately so returning to the window is never stale
      timer = window.setInterval(run, intervalMs)
    }

    const stop = () => {
      if (timer === undefined) return
      window.clearInterval(timer)
      timer = undefined
    }

    const onVisibility = () => {
      if (isAppVisible()) start()
      else stop()
    }

    onVisibility()
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('wa-visibility', onVisibility)

    return () => {
      cancelled = true
      stop()
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('wa-visibility', onVisibility)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, ...deps])
}

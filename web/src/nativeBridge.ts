// Typed access to the macOS shell (macapp/).
//
// The shell exposes two message channels on WKWebView:
//   - "app"      one-way notices from the page (which chat is on screen)
//   - "appAsync" request/response; the promise resolves with the shell's reply
//
// All of this is absent in a normal browser, so every helper degrades to
// "not running natively" rather than throwing.

type AsyncHandler = { postMessage: (body: unknown) => Promise<unknown> }

function asyncChannel(): AsyncHandler | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    webkit?: { messageHandlers?: { appAsync?: AsyncHandler } }
  }
  return w.webkit?.messageHandlers?.appAsync ?? null
}

/** True when the page is running inside the macOS app. */
export function isNativeShell(): boolean {
  if (typeof window === 'undefined') return false
  return (window as { __WA_NATIVE_SHELL__?: boolean }).__WA_NATIVE_SHELL__ === true
}

/** Does this Mac have Touch ID available to the app? */
export async function biometricsAvailable(): Promise<boolean> {
  const ch = asyncChannel()
  if (!ch) return false
  try {
    return (await ch.postMessage({ type: 'biometrics-available' })) === true
  } catch {
    return false
  }
}

/**
 * Runs the shell's Touch ID prompt and returns a hidden-chat unlock token.
 *
 * Replaces the browser's WebAuthn assertion, which cannot run in a WKWebView.
 * Throws with the shell's message on failure; 'cancelled' when the user
 * dismissed the Touch ID sheet.
 */
export async function nativeUnlock(pinPassedToken: string): Promise<string> {
  const ch = asyncChannel()
  if (!ch) throw new Error('Not running in the macOS app')

  let reply: unknown
  try {
    reply = await ch.postMessage({
      type: 'native-unlock',
      pin_passed_token: pinPassedToken,
    })
  } catch (e) {
    const msg = (e as Error)?.message || String(e)
    throw new Error(msg === 'cancelled' ? 'Touch ID cancelled' : msg)
  }

  const token = (reply as { unlock_token?: string } | null)?.unlock_token
  if (!token) throw new Error('The app did not return an unlock token')
  return token
}

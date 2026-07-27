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
 * Returns a hidden-chat unlock token from the macOS shell.
 *
 * With no argument this runs the Touch ID prompt — the primary credential in
 * the app, since WebAuthn cannot run in a WKWebView. Passing a pin-passed
 * handle takes the fallback path instead and skips the biometric prompt,
 * for when Touch ID is unavailable or was dismissed.
 *
 * Throws with the shell's message on failure; 'Touch ID cancelled' when the
 * user dismissed the sheet.
 */
export async function nativeUnlock(pinPassedToken?: string): Promise<string> {
  const ch = asyncChannel()
  if (!ch) throw new Error('Not running in the macOS app')

  let reply: unknown
  try {
    reply = await ch.postMessage(
      pinPassedToken
        ? { type: 'native-unlock', pin_passed_token: pinPassedToken }
        : { type: 'native-unlock' },
    )
  } catch (e) {
    const msg = (e as Error)?.message || String(e)
    throw new Error(msg === 'cancelled' ? 'Touch ID cancelled' : msg)
  }

  const token = (reply as { unlock_token?: string } | null)?.unlock_token
  if (!token) throw new Error('The app did not return an unlock token')
  return token
}

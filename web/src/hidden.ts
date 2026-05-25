// Hidden-chats client state + WebAuthn helpers.
//
// Two unlock flavours, mirroring the backend:
//   - GLOBAL token in sessionStorage — flips the whole UI into "private mode"
//     (chats list shows only hidden chats, SSE filter swaps).
//   - PER-CHAT tokens in an in-memory Map — opens ONE hidden chat without
//     flipping the UI. Used when the user clicks a mention chip for a hidden
//     contact and approves the fingerprint prompt.
//
// The fetch override in main.tsx picks the right token based on the URL +
// request body, so per-call attaching is automatic.

const TOKEN_KEY = 'wa.hiddenUnlock'

export function getUnlockToken(): string | null {
  try {
    return sessionStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

export function setUnlockToken(token: string | null) {
  try {
    if (token) sessionStorage.setItem(TOKEN_KEY, token)
    else sessionStorage.removeItem(TOKEN_KEY)
  } catch {}
  // Tell the rest of the app to refetch lists.
  window.dispatchEvent(new CustomEvent('wa.unlock-changed'))
}

export function isUnlocked(): boolean {
  return !!getUnlockToken()
}

// ─── Per-chat unlock registry ─────────────────────────────────────────
// In-memory only — opening a hidden chat shouldn't survive a refresh.

type ChatTok = { token: string; expiry: number }
const chatTokens = new Map<string, ChatTok>()

export function setChatUnlock(chatJID: string, token: string, ttlSeconds: number) {
  chatTokens.set(chatJID, { token, expiry: Date.now() + ttlSeconds * 1000 })
  window.dispatchEvent(new CustomEvent('wa.chat-unlock-changed', { detail: { chatJID } }))
}

export function clearChatUnlock(chatJID: string) {
  if (chatTokens.delete(chatJID)) {
    window.dispatchEvent(new CustomEvent('wa.chat-unlock-changed', { detail: { chatJID } }))
  }
}

export function getChatUnlockToken(chatJID: string): string | null {
  const e = chatTokens.get(chatJID)
  if (!e) return null
  if (e.expiry < Date.now()) {
    chatTokens.delete(chatJID)
    return null
  }
  return e.token
}

// pickTokenFor returns the right token to use for an API call referencing
// chatJID. Global token wins (if set); otherwise the per-chat token (if any).
export function pickTokenFor(chatJID: string | null | undefined): string | null {
  const g = getUnlockToken()
  if (g) return g
  if (!chatJID) return null
  return getChatUnlockToken(chatJID)
}

// authedFetch is a thin fetch wrapper that adds the X-Hidden-Unlock header
// when a global token is present. Per-chat tokens are attached automatically
// by the fetch override in main.tsx.
export function authedFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers || {})
  const tok = getUnlockToken()
  if (tok) headers.set('X-Hidden-Unlock', tok)
  return fetch(input, { ...init, headers })
}

// ─── WebAuthn helpers ──────────────────────────────────────────────────
// The server returns/accepts ArrayBuffer fields as base64url strings. The
// browser API works with ArrayBuffers. These helpers do the conversion.

function b64uToBytes(b64url: string): Uint8Array {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/')
  const pad = b64.length % 4 === 0 ? '' : '='.repeat(4 - (b64.length % 4))
  const bin = atob(b64 + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function bytesToB64u(buf: ArrayBuffer | Uint8Array): string {
  const u = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
  let s = ''
  for (let i = 0; i < u.length; i++) s += String.fromCharCode(u[i])
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

// Turn the server's PublicKeyCredentialCreationOptions (with base64url strings)
// into the structure the browser API wants (with ArrayBuffers).
export function decodeCreationOptions(opts: any): any {
  const out: any = { ...opts }
  out.challenge = b64uToBytes(opts.challenge)
  if (opts.user) out.user = { ...opts.user, id: b64uToBytes(opts.user.id) }
  if (opts.excludeCredentials) {
    out.excludeCredentials = opts.excludeCredentials.map((c: any) => ({
      ...c,
      id: b64uToBytes(c.id),
    }))
  }
  return out
}

export function decodeRequestOptions(opts: any): any {
  const out: any = { ...opts }
  out.challenge = b64uToBytes(opts.challenge)
  if (opts.allowCredentials) {
    out.allowCredentials = opts.allowCredentials.map((c: any) => ({
      ...c,
      id: b64uToBytes(c.id),
    }))
  }
  return out
}

// Turn the browser's credential response into the JSON the server expects.
export function encodeCredential(cred: PublicKeyCredential): any {
  const out: any = {
    id: cred.id,
    rawId: bytesToB64u(cred.rawId),
    type: cred.type,
    response: {},
  }
  const r = cred.response as any
  if ('attestationObject' in r) {
    out.response.attestationObject = bytesToB64u(r.attestationObject)
    out.response.clientDataJSON = bytesToB64u(r.clientDataJSON)
    if (r.getTransports) {
      try { out.response.transports = r.getTransports() } catch {}
    }
  } else if ('signature' in r) {
    out.response.signature = bytesToB64u(r.signature)
    out.response.clientDataJSON = bytesToB64u(r.clientDataJSON)
    out.response.authenticatorData = bytesToB64u(r.authenticatorData)
    if (r.userHandle) out.response.userHandle = bytesToB64u(r.userHandle)
  }
  if (cred.authenticatorAttachment) {
    out.authenticatorAttachment = cred.authenticatorAttachment
  }
  // Some servers want clientExtensionResults; include if available.
  if ((cred as any).getClientExtensionResults) {
    try { out.clientExtensionResults = (cred as any).getClientExtensionResults() } catch {}
  }
  return out
}

// assertSecureContext throws a clear, actionable error if WebAuthn isn't
// available. Browsers only expose navigator.credentials over HTTPS or on a
// localhost origin — bare `http://*.test` doesn't qualify.
function assertSecureContext() {
  if (typeof navigator === 'undefined' || !('credentials' in navigator) || !navigator.credentials) {
    const host = typeof location !== 'undefined' ? location.host : ''
    const fix = host && !host.startsWith('localhost') && !host.startsWith('127.')
      ? `Open the bridge at http://localhost:8082 (current: ${location.protocol}//${host}). WebAuthn requires HTTPS or localhost.`
      : 'WebAuthn is not available in this browser.'
    throw new Error(fix)
  }
}

// Run a Touch ID registration. Returns the encoded credential to POST back.
export async function webauthnRegister(opts: any): Promise<any> {
  assertSecureContext()
  const cred = (await navigator.credentials.create({
    publicKey: decodeCreationOptions(opts),
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('Touch ID was cancelled')
  return encodeCredential(cred)
}

// Run a Touch ID assertion. Returns the encoded credential to POST back.
export async function webauthnAssert(opts: any): Promise<any> {
  assertSecureContext()
  const cred = (await navigator.credentials.get({
    publicKey: decodeRequestOptions(opts),
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('Touch ID was cancelled')
  return encodeCredential(cred)
}

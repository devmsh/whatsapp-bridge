import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { getUnlockToken, getChatUnlockToken } from './hidden'

// Inject the right hidden-chats unlock token into every same-origin /api/v2/*
// call without each call site knowing about it.
//
//   - A GLOBAL token (sessionStorage) wins and unlocks anything.
//   - Otherwise, when the request references a specific chat (via ?chat_jid=
//     in the URL, a /chats/<jid> path, ?jid= for send, or a chat_jid/jid
//     field in a JSON POST body), we attach the per-chat token if we have
//     one — opens one hidden chat without flipping the whole UI.
//   - Media URLs (/api/v2/media/...) can't carry a header from <audio>/<img>
//     tags, so they bake the token in via ?unlock= at the call site.

const _origFetch = window.fetch

function extractChatJID(url: string, init?: RequestInit): string | null {
  try {
    const u = new URL(url, location.origin)
    if (!u.pathname.startsWith('/api/v2/')) return null
    const q = u.searchParams.get('chat_jid') || u.searchParams.get('jid')
    if (q) return q
    // /api/v2/chats/<JID> (with or without subpath)
    const m = u.pathname.match(/^\/api\/v2\/chats\/([^/]+)/)
    if (m) return decodeURIComponent(m[1])
    // POST/PUT JSON body
    if (init?.body && typeof init.body === 'string') {
      try {
        const obj = JSON.parse(init.body)
        if (typeof obj?.chat_jid === 'string') return obj.chat_jid
        if (typeof obj?.jid === 'string') return obj.jid
      } catch {}
    }
  } catch {}
  return null
}

window.fetch = function (input, init) {
  let path = ''
  if (typeof input === 'string') path = input
  else if (input instanceof URL) path = input.pathname
  else if (input instanceof Request) path = input.url
  const isApi =
    path.startsWith('/api/v2/') ||
    (path.includes('://') && new URL(path).pathname.startsWith('/api/v2/'))
  if (!isApi) return _origFetch(input as any, init)

  // Global token wins.
  let tok = getUnlockToken()
  if (!tok) {
    const jid = extractChatJID(path, init)
    if (jid) tok = getChatUnlockToken(jid)
  }
  if (!tok) return _origFetch(input as any, init)
  const headers = new Headers(
    (init && init.headers) || (input instanceof Request ? input.headers : undefined),
  )
  headers.set('X-Hidden-Unlock', tok)
  return _origFetch(input as any, { ...(init || {}), headers })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

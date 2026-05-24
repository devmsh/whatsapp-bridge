import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { getUnlockToken } from './hidden'

// Inject the hidden-chats unlock token into every same-origin /api/v2/* call,
// so any handler can use it without each call site knowing about it.
const _origFetch = window.fetch
window.fetch = function (input, init) {
  let path = ''
  if (typeof input === 'string') path = input
  else if (input instanceof URL) path = input.pathname
  else if (input instanceof Request) path = input.url
  const isApi = path.startsWith('/api/v2/') || path.includes('://') && new URL(path).pathname.startsWith('/api/v2/')
  if (!isApi) return _origFetch(input as any, init)
  const tok = getUnlockToken()
  if (!tok) return _origFetch(input as any, init)
  const headers = new Headers((init && init.headers) || (input instanceof Request ? input.headers : undefined))
  headers.set('X-Hidden-Unlock', tok)
  return _origFetch(input as any, { ...(init || {}), headers })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

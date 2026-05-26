import { useEffect, useMemo } from 'react'
import type { Chat } from '../api'

// useUnreadBadge keeps the browser tab title and favicon in sync with the
// total unread count across all chats — same affordance WA Web uses so you
// can spot a new message from another tab without focusing this one.
//
// Two pieces:
//   • document.title is "(N) WhatsApp Bridge" when N > 0, "WhatsApp Bridge"
//     otherwise. Caps at "99+" so a backlog doesn't blow up the tab strip.
//   • favicon is a canvas-painted chat-bubble icon (emerald, three white
//     dots) with a red badge in the corner carrying the count when N > 0.
//     Generated as a data URL and swapped in via <link rel="icon">, no
//     asset to ship.
//
// Muted chats are excluded — they explicitly opted out of attention, so
// they shouldn't drive a tab-strip badge either. Same as WA.
const BASE_TITLE = 'WhatsApp Bridge'

export function useUnreadBadge(chats: Chat[]) {
  const totalUnread = useMemo(() => {
    let sum = 0
    for (const c of chats) {
      if (c.is_muted) continue
      sum += c.unread_count || 0
    }
    return sum
  }, [chats])

  useEffect(() => {
    const label = totalUnread > 99 ? '99+' : String(totalUnread)
    document.title = totalUnread > 0 ? `(${label}) ${BASE_TITLE}` : BASE_TITLE
    setFavicon(totalUnread)
    // No cleanup — we want the badge to persist across React reconciliation
    // bumps. The next call overwrites cleanly.
  }, [totalUnread])
}

// setFavicon paints a small chat-bubble icon onto a 64×64 canvas, adds an
// optional red unread badge, and swaps the result into <link rel="icon">.
// We do this every effect rather than caching because the count usually
// changes only on real events (incoming message, mark-as-read) and the
// paint is sub-millisecond.
function setFavicon(unread: number) {
  if (typeof document === 'undefined') return
  try {
    const size = 64
    const canvas = document.createElement('canvas')
    canvas.width = size
    canvas.height = size
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Emerald chat bubble body. Rounded rect; roundRect is broadly
    // supported in modern browsers (2023+), fall back to a plain rect
    // when the canvas doesn't have it.
    ctx.fillStyle = '#10b981'
    if (typeof (ctx as unknown as { roundRect?: unknown }).roundRect === 'function') {
      ctx.beginPath()
      ;(ctx as CanvasRenderingContext2D & { roundRect: (x: number, y: number, w: number, h: number, r: number) => void })
        .roundRect(4, 6, 56, 44, 12)
      ctx.fill()
    } else {
      ctx.fillRect(4, 6, 56, 44)
    }
    // Tail
    ctx.beginPath()
    ctx.moveTo(14, 48)
    ctx.lineTo(8, 58)
    ctx.lineTo(22, 50)
    ctx.closePath()
    ctx.fill()

    // Three dots
    ctx.fillStyle = '#ffffff'
    for (const x of [20, 32, 44]) {
      ctx.beginPath()
      ctx.arc(x, 28, 3, 0, Math.PI * 2)
      ctx.fill()
    }

    // Red unread badge — top-right corner, with the count or "99+".
    if (unread > 0) {
      const cx = 48
      const cy = 18
      const r = 16
      ctx.fillStyle = '#ef4444'
      ctx.beginPath()
      ctx.arc(cx, cy, r, 0, Math.PI * 2)
      ctx.fill()
      ctx.fillStyle = '#ffffff'
      const label = unread > 99 ? '99+' : String(unread)
      ctx.font = `bold ${label.length > 2 ? 14 : 20}px system-ui, sans-serif`
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillText(label, cx, cy + 1)
    }

    const url = canvas.toDataURL('image/png')
    let link = document.querySelector("link[rel='icon']") as HTMLLinkElement | null
    if (!link) {
      link = document.createElement('link')
      link.rel = 'icon'
      document.head.appendChild(link)
    }
    link.type = 'image/png'
    link.href = url
  } catch {
    // Some browsers / sandboxes block canvas → data URL; in that case the
    // tab title still updates and we just lose the favicon dot.
  }
}

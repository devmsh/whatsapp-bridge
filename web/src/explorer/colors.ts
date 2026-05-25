import type { Circle } from '../api'

// Curated, visually distinct palette for circles.
export const CIRCLE_COLORS = [
  '#22c55e', // green
  '#3b82f6', // blue
  '#a855f7', // purple
  '#ef4444', // red
  '#f59e0b', // amber
  '#14b8a6', // teal
  '#ec4899', // pink
  '#8b5cf6', // violet
  '#06b6d4', // cyan
  '#84cc16', // lime
  '#f97316', // orange
  '#64748b', // slate
]

// pickColor returns the least-used palette color among existing circles, so new
// circles get distinct colors. Falls back to a random palette color.
export function pickColor(circles: Circle[]): string {
  const used = new Map<string, number>()
  for (const c of CIRCLE_COLORS) used.set(c, 0)
  for (const c of circles) {
    if (used.has(c.color)) used.set(c.color, (used.get(c.color) || 0) + 1)
  }
  let best = CIRCLE_COLORS[0]
  let min = Infinity
  for (const c of CIRCLE_COLORS) {
    const n = used.get(c) ?? 0
    if (n < min) {
      min = n
      best = c
    }
  }
  return best
}

// Saturated palette used for the per-sender name color in group bubbles, the
// way official WhatsApp distinguishes speakers in a busy group. Tuned to read
// well on the dark thread background.
const SENDER_COLORS = [
  '#06cf9c', // emerald
  '#ff6b6b', // coral
  '#ffd166', // amber
  '#7cc7ff', // sky
  '#c792ea', // lilac
  '#ff9e64', // orange
  '#9ece6a', // lime
  '#f78fb3', // pink
  '#7dd3fc', // cyan
  '#facc15', // gold
  '#a78bfa', // violet
  '#5eead4', // teal
  '#fda4af', // rose
  '#bef264', // chartreuse
  '#fbbf24', // sun
  '#67e8f9', // ice
  '#f0abfc', // fuchsia
  '#86efac', // mint
]

// senderColor returns a stable color from SENDER_COLORS for a given JID, so a
// participant keeps the same color across every group they appear in. The hash
// is a simple FNV-1a over the JID — collisions are fine; a busy group just
// gets a couple of doubles, exactly like WhatsApp itself.
export function senderColor(jid: string): string {
  if (!jid) return SENDER_COLORS[0]
  let h = 2166136261 >>> 0
  for (let i = 0; i < jid.length; i++) {
    h ^= jid.charCodeAt(i)
    h = Math.imul(h, 16777619) >>> 0
  }
  return SENDER_COLORS[h % SENDER_COLORS.length]
}

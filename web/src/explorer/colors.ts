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

import type { Circle } from '../api'

// A circle flattened out of the tree, ready to drop into a <select>.
export interface CircleOption {
  id: number
  name: string
  depth: number
  /** Indented name. A native <option> cannot nest, so the depth is drawn in. */
  label: string
}

// Non-breaking spaces: a plain space collapses inside an <option> in some
// browsers, which would flatten the tree back out.
const PAD = '   '

function byName(a: Circle, b: Circle) {
  return a.name.localeCompare(b.name)
}

// flattenCircleTree walks the circle tree depth-first and returns one row per
// circle, in the order you see them in the sidebar, each carrying its depth.
// A picker built from this reads the same as the tree: "ZOD" shows under "BIV",
// which shows under "OneStudio", instead of a flat A–Z list where a sub-circle
// looks like a top-level one.
//
// Circles are a graph, not a strict tree — a circle can sit under more than one
// parent, and a bad edge could form a loop. `path` stops a loop from recursing
// forever, and anything never reached (an orphan, or a member of a cycle) is
// appended at the end so a circle can never vanish from the picker.
export function flattenCircleTree(circles: Circle[]): CircleOption[] {
  const byId = new Map(circles.map((c) => [c.id, c]))
  const isChild = new Set<number>()
  for (const c of circles) for (const id of c.child_circles || []) isChild.add(id)

  const out: CircleOption[] = []
  const placed = new Set<number>()

  function walk(c: Circle, depth: number, path: Set<number>) {
    out.push({
      id: c.id,
      name: c.name,
      depth,
      label: depth === 0 ? c.name : PAD.repeat(depth) + '↳ ' + c.name,
    })
    placed.add(c.id)
    const kids = (c.child_circles || [])
      .map((id) => byId.get(id))
      .filter((x): x is Circle => !!x && !path.has(x.id))
      .sort(byName)
    for (const k of kids) walk(k, depth + 1, new Set([...path, c.id]))
  }

  for (const root of circles.filter((c) => !isChild.has(c.id)).sort(byName)) {
    walk(root, 0, new Set())
  }
  for (const c of [...circles].sort(byName)) {
    if (!placed.has(c.id)) {
      out.push({ id: c.id, name: c.name, depth: 0, label: c.name })
      placed.add(c.id)
    }
  }
  return out
}

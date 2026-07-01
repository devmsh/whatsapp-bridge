# Plan — Focus Mode Iter 4: Sub-Circle Drill-Down + Circle-Management Fold-In

- **Spec**: `docs/superpowers/specs/2026-07-01-focus-mode-iter4-drilldown-management-foldin-design.md`
- **Branch**: `worktree-turbo+iter-04-drilldown-management-foldin-2026-07-01`
- **Execution**: turbo subagent-driven, per-wave parallel implementers + serialized committer.
- **QA**: skipped this iteration (no `web/` test script/files; unchanged since iters 1-3). See `dag_warnings`.
- **Lint / build gates**: no web lint configured. Per task, run in `web/`: `npx tsc -b` AND `npx vite build` both succeed.
- **Commit granularity**: per task; each task ends with `verification-before-completion` evidence and a commit.

## Waves overview

Both tasks write `web/src/explorer/FocusMode.tsx`, so they CANNOT share a wave
(write-write collision). They are serialized: T01 in wave 0, T02 in wave 1.

- **Wave 0** (1 task): T01 — sub-circle drill-down breadcrumb (`FocusMode.tsx` only).
- **Wave 1** (1 task): T02 — circle-management fold-in + retire the standalone
  `CircleView` path (`Explorer.tsx` + `FocusMode.tsx`).

`wave_count = 2`, `max_wave_width = 1`.

---

## Wave 0

### T01 — Sub-circle drill-down breadcrumb

- **id**: T01
- **wave**: 0
- **depends_on**: []
- **files_write**:
  - `web/src/explorer/FocusMode.tsx`
- **files_read**:
  - `web/src/api.ts`

**Steps**

1. `web/src/explorer/FocusMode.tsx`: in the header (`FocusMode.tsx:77-92`), add a
   breadcrumb strip that reads only the existing `circle` /
   `circles` / `onSwitchCircle` values (`circle` is already computed at line 68;
   `circles` and `onSwitchCircle` are already props — NO new prop, NO new fetch).
2. **Parents**: when `(circle?.parent_ids?.length ?? 0) > 0`, map each parent id
   to `circles.find((c) => c.id === parentId)`; for each resolved parent render a
   clickable "↑ <parent name>" link calling `onSwitchCircle(parentId)`. Skip ids
   that don't resolve (render nothing for them). This re-targets Focus Mode; it
   does NOT exit.
3. **Children**: when `(circle?.child_circles?.length ?? 0) > 0`, map each child
   id to `circles.find((c) => c.id === childId)`; render each resolved child as a
   small "↳ <child name>" button calling `onSwitchCircle(childId)`. Skip
   unresolved ids.
4. `parent_ids` and `child_circles` are OPTIONAL on `Circle` (`api.ts:408-409`) —
   guard every access with `?.` / `?? 0`; never index a possibly-undefined array.
5. Keep this visually distinct from and alongside the existing `FocusSwitcher`
   (`FocusMode.tsx:85`) — the breadcrumb is a compact hierarchy aid, not a
   replacement for the flat all-circles dropdown. Do NOT modify `FocusSwitcher`.

**Verification**: in `web/`, `npx tsc -b` and `npx vite build` both succeed. Commit.

---

## Wave 1

### T02 — Circle-management fold-in + retire the standalone CircleView path

- **id**: T02
- **wave**: 1
- **depends_on**: [T01]
- **files_write**:
  - `web/src/explorer/Explorer.tsx`
  - `web/src/explorer/FocusMode.tsx`
- **files_read**:
  - `web/src/explorer/CircleView.tsx`
  - `web/src/explorer/CirclesPanel.tsx`

> `depends_on: [T01]` is a file-ownership dependency: T01 and T02 both write
> `FocusMode.tsx`, so T02 must run in a strictly later wave. There is no logical
> coupling to the breadcrumb code beyond co-editing the file.

**Steps — `web/src/explorer/Explorer.tsx`**

1. Add intent state (plain `useState`, NOT persisted to localStorage), near the
   other Focus state (around `Explorer.tsx:63-76`):
   `const [focusManagingIntent, setFocusManagingIntent] = useState(false)`.

2. Redirect `openCircle` (`Explorer.tsx:552-557`). Replace its body's
   `setSelectedCircle(id)` with
   `setFocusManagingIntent(true)` **then** `setFocusCircleId(id)`, giving:
   ```ts
   const openCircle = useCallback((id: number) => {
     setRecoOpen(false)
     setSelected(null)
     setSelectedTask(null)
     setFocusManagingIntent(true)
     setFocusCircleId(id)
   }, [])
   ```

3. Wrap the three non-exit direct `setFocusCircleId(...)` set sites to reset the
   intent flag first (grep-verified; the review named the first two, the third is
   the defensive extra):
   - `Explorer.tsx:621` `onSwitchCircle={setFocusCircleId}` →
     `onSwitchCircle={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`.
   - `Explorer.tsx:898` `<FocusSwitcher ... onSelect={setFocusCircleId} />` →
     `onSelect={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`.
   - `Explorer.tsx:619` `onOpenCircle={(id) => setFocusCircleId(id)}` →
     `onOpenCircle={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`.
   Leave the exit sites (`setFocusCircleId(null)` at lines 440, 608, 612, 616,
   620) unchanged.

4. Add four props to the `<FocusMode ... />` usage (`Explorer.tsx:585-623`):
   - `initialManaging={focusManagingIntent}`
   - `contacts={contacts}`
   - `groups={groups}`
   - `onOpenTasks={(id) => { setFocusCircleId(null); openCircleTasks(id) }}`

5. Retire the standalone `selectedCircle` path — remove EVERY reference (the
   brief's list was incomplete; the full grep-verified set is below). After this
   step, `grep -n "selectedCircle\|setSelectedCircle" web/src/explorer/Explorer.tsx`
   MUST return zero matches:
   - Remove the declaration at `Explorer.tsx:63`
     (`const [selectedCircle, setSelectedCircle] = useState<number | null>(null)`).
   - Remove `setSelectedCircle(null)` from all five functions: `openTask`
     (line 362), `openChatTasks` (line 372), `openCircleTasks` (line 535),
     `openContactDM` (line 545), `openReco` (line 561).
   - Remove `selectedCircle != null` from the `detailOpen` expression (line 572);
     it becomes `recoOpen || selected != null || selectedTask != null`.
   - Remove `if (selectedCircle != null) return setSelectedCircle(null)` from
     `closeMobileDetail` (line 579).
   - Change `<CirclesPanel ... selected={selectedCircle} ... />` (line 946) to
     `selected={null}` (the prop stays required on `CirclesPanel`'s type —
     `CirclesPanel.tsx:10-26` — just always `null` now).
   - Collapse the render tail `) : selectedCircle != null ? ( <CircleView ... /> ) : ( <EmptyState /> )`
     (lines 1059-1074) to `) : ( <EmptyState /> )`. Keep the ternary chain above
     it (`recoOpen ? … : tab === 'tasks' ? … : selected ? <MessageThread/> : …`)
     intact.
   - Remove `import { CircleView } from './CircleView'` (`Explorer.tsx:18`).

**Steps — `web/src/explorer/FocusMode.tsx`**

6. Import `CircleView` from `./CircleView`. Extend the type import to
   `import type { Chat, Circle, Contact, Group, Message, Tag, Task } from '../api'`.

7. Add four props to the component's prop list and type:
   `contacts: Contact[]`, `groups: Group[]`, `onOpenTasks: (id: number) => void`,
   `initialManaging: boolean`.

8. Add `const [managing, setManaging] = useState(initialManaging)` — a plain
   `useState` initializer. Do NOT add a `[circleId]`-keyed `useEffect` for it
   (that would wrongly reset Manage mode on every circle switch; the initializer
   correctly captures entry intent while preserving mid-session state — see spec
   rationale).

9. Add a "⚙ Manage" toggle button in the header near "Exit Focus"
   (`FocusMode.tsx:86-91`) that toggles `managing`.

10. Render branch: when `managing` is `true`, render `CircleView` in place of the
    normal 3-panel-left / chat-or-thread-right body (the grid block at
    `FocusMode.tsx:100-174`), a full replace (not an overlay). Keep the header
    (breadcrumb + Manage toggle + switcher + Exit) rendered above both modes:
    ```tsx
    <CircleView
      circleId={circleId}
      circles={circles}
      chats={chats}
      contacts={contacts}
      groups={groups}
      nameMap={nameMap}
      allTags={allTags}
      onTagsChanged={onTagsChanged}
      onOpenChat={onOpenChat}
      onOpenCircle={onOpenCircle}
      onOpenTasks={onOpenTasks}
      onChanged={onCirclesChanged}
      onDeleted={() => { setManaging(false); onExit() }}
    />
    ```
    All of `circles`, `chats`, `nameMap`, `allTags`, `onTagsChanged`,
    `onOpenChat`, `onOpenCircle`, `onCirclesChanged` are already `FocusMode` props
    — reuse as-is. (`FocusMode`'s `onOpenChat` is `(jid, draft?) => void` and
    `CircleView` expects `(jid) => void` — assignable; no cast needed.)

**Verification**: in `web/`, `npx tsc -b` and `npx vite build` both succeed;
`grep -n "selectedCircle\|setSelectedCircle" web/src/explorer/Explorer.tsx`
returns nothing. Commit.

---

## Self-validation

1. **No write-write collision in a wave.**
   - Wave 0 `files_write`: T01 → `web/src/explorer/FocusMode.tsx`. Single task; no
     collision. PASS.
   - Wave 1 `files_write`: T02 → `web/src/explorer/Explorer.tsx`,
     `web/src/explorer/FocusMode.tsx`. Single task; no collision. PASS.
   - `FocusMode.tsx` is written by BOTH T01 (wave 0) and T02 (wave 1) — legal
     because they are in different waves (serialized by `depends_on`). PASS.
2. **Wave monotonicity.** T02 (wave 1) `depends_on [T01]` (wave 0); 1 > 0. PASS.
   T01 (wave 0) has empty `depends_on`. PASS.
3. **No placeholders.** Every `files_write` / `files_read` entry is a concrete
   path (no globs, no TBD). The only `<...>` occurrences are inside illustrative
   TSX/label snippets ("↑ <parent name>", the JSX examples) that explicitly show
   dynamic content, not in path fields. PASS.
4. **Wave 0 independence.** T01 has empty `depends_on`. PASS.

DAG validation: PASS.

## Notes / warnings

- **QA**: no `web/` test script or test files exist (unchanged since iters 1-3),
  so QA is skipped this iteration. Both tasks are pure client-side React;
  `CircleView` is reused verbatim, so its existing (manually verified) behavior is
  unchanged. Manual smoke recommended post-merge: (a) circle row click still lands
  directly on Manage; (b) FocusSwitcher lands on the dashboard; (c) breadcrumb
  parent/child links re-target without exiting; (d) deleting a circle from Manage
  exits Focus Mode.
- **Manifest deviation** (projections are estimates): T01 does NOT write
  `FocusSwitcher.tsx` (breadcrumb built from existing props); T02 adds NO
  `FocusManage.tsx` and does NOT edit `CircleView.tsx` / `CircleSettings.tsx`
  (reused verbatim). Net files touched this iteration: `FocusMode.tsx` (T01+T02),
  `Explorer.tsx` (T02). See the spec's "Deviations from manifest projection".
- **Completion check for the retirement** (T02 step 5): a zero-match grep for
  `selectedCircle` / `setSelectedCircle` in `Explorer.tsx` is the objective
  signal that no orphaned reference remains.

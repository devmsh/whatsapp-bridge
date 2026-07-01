# Focus Mode — Iter 4: Sub-Circle Drill-Down + Circle-Management Fold-In

- **Date**: 2026-07-01
- **Iteration**: iter-04-drilldown-management-foldin (4 of 4 — the FINAL iteration)
- **Manifest**: `docs/superpowers/turbo-manifests/i-need-to-develop-a-focus-mode-where-i-can-choose-manifest.yml`
- **Branch**: `worktree-turbo+iter-04-drilldown-management-foldin-2026-07-01` (from `main`, which has iters 1-3 merged)
- **Status**: ready to build

## Summary

Two changes finish the Focus Mode feature:

1. **Sub-circle drill-down** — a small hierarchy breadcrumb inside Focus Mode's
   header. Parents of the focused circle show as "↑ <parent>" links; children
   show as "↳ <child>" quick-links. Both re-target Focus Mode via the existing
   `onSwitchCircle` prop (they do NOT exit Focus Mode). This is additive to
   `FocusMode.tsx` only.

2. **Circle-management fold-in** — the standalone `CircleView` management screen
   (rename, members, keyword suggestions, sub-circle creation, task extraction +
   history, export-as-zip, notes/keywords) becomes a "Manage" mode INSIDE Focus
   Mode. `CircleView.tsx` is reused verbatim (zero source changes). The old
   separately-reachable management screen is retired from `Explorer.tsx`.

The single real risk is a UX regression: every existing "click a circle → land
on its management screen" entry point must still land directly in Manage mode,
not on Focus Mode's normal dashboard. This is solved with an explicit
`focusManagingIntent` / `initialManaging` flag (see Design → T02).

## Goal

- A hierarchy breadcrumb in Focus Mode's header, distinct from the flat
  `FocusSwitcher`, that answers "where am I in the circle tree?" and lets the
  user jump up to a parent or down into a child without leaving Focus Mode.
- `CircleView`'s full management surface available as a "⚙ Manage" toggle inside
  Focus Mode.
- `CircleView` retired as a standalone `<main>` render branch in `Explorer.tsx`
  and all its dead `selectedCircle` state removed — WITHOUT changing the
  behavior of any entry point that today opens a circle's management directly.

## Non-goals

- **No source changes to `CircleView.tsx` or `CircleSettings.tsx`.** Confirmed
  by adversarial review: `CircleView` destructures exactly
  `{circleId, circles, chats, contacts, groups, nameMap, allTags, onTagsChanged,
  onOpenChat, onOpenCircle, onOpenTasks, onChanged, onDeleted}`
  (`CircleView.tsx:28-56`) and its internal `CircleSettings` / `ExtractionsModal`
  usages are fully self-contained — no reach-up into `Explorer.tsx`. It drops
  into Focus Mode unchanged.
- **No new `FocusManage.tsx` file.** The manifest *projected* a `FocusManage.tsx`
  wrapper plus edits to `CircleView.tsx` / `CircleSettings.tsx` / `FocusSwitcher.tsx`;
  the finalized (simpler) design reuses `CircleView` directly and touches only
  `FocusMode.tsx` + `Explorer.tsx`. See "Deviations from manifest projection".
- **No `FocusSwitcher.tsx` change.** The breadcrumb is a separate, smaller
  affordance built inside `FocusMode.tsx` from the existing `circles` /
  `onSwitchCircle` props. `FocusSwitcher` (the flat all-circles dropdown) is
  untouched.
- **No mobile-responsive redesign of Focus Mode.** Out of scope — see
  "Known limitation" and "Open questions".

## Design

### T01 — Sub-circle drill-down (breadcrumb), `FocusMode.tsx` only

`FocusMode` already computes `const circle = circles.find((c) => c.id === circleId)`
(`FocusMode.tsx:68`). Add a breadcrumb strip in the header area using only the
existing `circles` and `onSwitchCircle` props (no new prop, no new fetch):

- **Parents** — if `(circle?.parent_ids?.length ?? 0) > 0`, render one
  "↑ <parent name>" link per parent id. Resolve each name with
  `circles.find((c) => c.id === parentId)`; on click call
  `onSwitchCircle(parentId)`. This re-targets Focus Mode; it does NOT exit.
- **Children** — if `(circle?.child_circles?.length ?? 0) > 0`, render each as a
  small "↳ <child name>" quick-link button calling `onSwitchCircle(childId)`.

`parent_ids` and `child_circles` are **optional** fields on the `Circle` type
(`api.ts:408-409`) — guard every access with `?.` / `?? 0`. Unresolved ids
(parent/child not present in `circles`) are skipped (the `.find` returns
`undefined`; render nothing for that id rather than a broken chip).

This is DISTINCT from `FocusSwitcher` (which lists every circle flatly): the
breadcrumb is a compact "where am I in the hierarchy" aid rendered alongside it.

**Files**: writes `web/src/explorer/FocusMode.tsx`; reads `web/src/api.ts` (for
the `parent_ids` / `child_circles` fields on `Circle`).

### T02 — Circle-management fold-in + retire the standalone path

Two files change: `Explorer.tsx` (retire the standalone screen + add the intent
flag + thread new props) and `FocusMode.tsx` (add Manage mode). Because T01 also
writes `FocusMode.tsx`, **T02 must run in a later wave than T01** (file-ownership
dependency; they never share a wave).

#### The regression this fix prevents

Naively pointing `openCircle` straight at `setFocusCircleId` would silently
change "click a circle → land on its management screen" into "click a circle →
land on Focus Mode's normal dashboard, management one extra click away". Four
entry points rely on the direct-to-management behavior today: `CirclesPanel` row
clicks, `SearchBar` circle results, `RecommendationsView`, and the normal
(non-focused) `MessageThread`'s `onOpenCircle`. All four flow through
`openCircle`. The fix routes an explicit **intent** flag so they keep landing in
Manage mode, while switcher / breadcrumb navigation (already inside Focus Mode)
does not force Manage.

#### `Explorer.tsx` changes

1. **Add intent state** (plain, NOT persisted to localStorage — a
   restored/resumed session must resume on the normal dashboard, never stuck in
   Manage):
   `const [focusManagingIntent, setFocusManagingIntent] = useState(false)`.

2. **Redirect `openCircle`** (`Explorer.tsx:552-557`). Current body:
   `setRecoOpen(false); setSelected(null); setSelectedTask(null); setSelectedCircle(id)`.
   New body:
   `setRecoOpen(false); setSelected(null); setSelectedTask(null); setFocusManagingIntent(true); setFocusCircleId(id)`.
   Every existing `openCircle(id)` call site now enters Focus Mode with Manage
   already open — the correct, non-regressive behavior — with zero per-call-site
   changes.

3. **Guard the non-exit direct `setFocusCircleId` set sites** so a stale `true`
   from a prior `openCircle` cannot silently reopen Manage on an unrelated later
   navigation. Grep-verified, there are exactly three non-exit set sites; wrap
   each to reset the flag first:
   - `Explorer.tsx:621` `onSwitchCircle={setFocusCircleId}` →
     `onSwitchCircle={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`
     (named by the review).
   - `Explorer.tsx:898` normal-mode `FocusSwitcher` `onSelect={setFocusCircleId}` →
     `onSelect={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`
     (named by the review).
   - `Explorer.tsx:619` `onOpenCircle={(id) => setFocusCircleId(id)}` →
     `onOpenCircle={(id) => { setFocusManagingIntent(false); setFocusCircleId(id) }}`
     (the third non-exit set site; wrapped defensively per the review's "reset at
     every set site costs nothing and removes any doubt" guidance).

   > The exit sites — lines 440, 608, 612, 616, 620 (each `setFocusCircleId(null)`)
   > — are left as-is; setting the flag while leaving Focus Mode is pointless. The
   > reset is only load-bearing on the null→non-null (re-entry) transition, because
   > `FocusMode` reads `initialManaging` only in a `useState` initializer (below).

4. **Thread new props into the `<FocusMode ... />` usage** (`Explorer.tsx:585-623`):
   - `initialManaging={focusManagingIntent}`
   - `contacts={contacts}` (Explorer-level state already; pure passthrough)
   - `groups={groups}` (Explorer-level state already; pure passthrough)
   - `onOpenTasks={(id) => { setFocusCircleId(null); openCircleTasks(id) }}` — a
     NEW exit-wrapped callback, same pattern as the existing `onOpenTask` /
     `onOpenChatTasks` / `onOpenChat` wrappers on this usage.

5. **Retire the dead standalone path completely.** `selectedCircle` and every
   reference to it must be removed. The exhaustive, grep-verified list follows —
   **note the brief's original enumeration ("openCircleTasks, openContactDM,
   openReco") was INCOMPLETE; `openTask` and `openChatTasks` also call
   `setSelectedCircle(null)`**:
   - Remove the declaration `const [selectedCircle, setSelectedCircle] = useState<number | null>(null)` (`Explorer.tsx:63`).
   - Remove `setSelectedCircle(null)` from **all five** functions:
     `openTask` (line 362), `openChatTasks` (line 372), `openCircleTasks`
     (line 535), `openContactDM` (line 545), `openReco` (line 561).
   - Remove `selectedCircle != null` from the `detailOpen` computation (line 572).
   - Remove `if (selectedCircle != null) return setSelectedCircle(null)` from
     `closeMobileDetail` (line 579).
   - Change `<CirclesPanel ... selected={selectedCircle} ... />` (line 946) to
     `selected={null}`. The `selected` prop stays required on `CirclesPanel`'s
     type (`CirclesPanel.tsx:10-26`) — "the currently open circle" no longer
     exists, so it is always `null` now.
   - Collapse the render branch (lines 1059-1074)
     `) : selectedCircle != null ? ( <CircleView ... /> ) : ( <EmptyState /> )`
     to just `) : ( <EmptyState /> )`. This removes the last read of
     `selectedCircle` (`circleId={selectedCircle}`) and the last
     `setSelectedCircle(null)` (`onDeleted`). Preserve the surrounding
     `recoOpen ? … : tab === 'tasks' ? … : selected ? <MessageThread/> : …`
     ternary chain above it exactly.
   - Remove the now-unused `import { CircleView } from './CircleView'`
     (`Explorer.tsx:18`) — `CircleView` moves to `FocusMode.tsx`.

   After this task, `grep -n "selectedCircle\|setSelectedCircle" Explorer.tsx`
   must return **zero** matches. This is the completion check for step 5.

#### `FocusMode.tsx` changes

1. **New props on the type**: `contacts: Contact[]`, `groups: Group[]`,
   `onOpenTasks: (id: number) => void`, `initialManaging: boolean`. Import
   `Contact` and `Group` from `../api` (current import is
   `import type { Chat, Circle, Message, Tag, Task } from '../api'`).

2. **Manage state**: `const [managing, setManaging] = useState(initialManaging)`
   — a plain `useState` initializer, intentionally NOT a `[circleId]`-keyed
   `useEffect`. `FocusMode` only mounts fresh on a genuine null→non-null entry
   (Explorer's early-return stops rendering it on exit), so the initializer
   captures "was Manage-intent set at entry". Switching circles mid-session (via
   `FocusSwitcher` or the T01 breadcrumb) changes `circleId` WITHOUT unmounting
   `FocusMode`, which correctly PRESERVES the user's current `managing` state.
   Adding a `[circleId]` effect here would wrongly reset `managing` on every
   switch — do not add one.

3. **Header toggle**: a "⚙ Manage" button near "Exit Focus"
   (`FocusMode.tsx:86-91`) that toggles `managing`.

4. **Render branch**: when `managing` is `true`, render `CircleView` INSTEAD of
   the normal 3-panel-left / chat-or-thread-right body (a full replace, not an
   overlay — `CircleView` is itself a self-contained scrollable screen; nesting
   it in the grid would create double-scroll):
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
   `circles`, `chats`, `nameMap`, `allTags`, `onTagsChanged`, `onOpenChat`,
   `onOpenCircle`, `onCirclesChanged` are ALREADY `FocusMode` props from iters
   1-3 — reuse as-is; do not re-thread them. (`FocusMode`'s `onOpenChat` is
   `(jid, draft?) => void`; `CircleView` expects `(jid) => void` — assignable, no
   type error.) The header (with breadcrumb + Manage toggle + switcher + Exit)
   stays rendered above both the normal body and the Manage body.

5. **Import** `CircleView` from `./CircleView`.

**Files**: writes `web/src/explorer/Explorer.tsx` and
`web/src/explorer/FocusMode.tsx`; reads `web/src/explorer/CircleView.tsx` and
`web/src/explorer/CirclesPanel.tsx`.

## Data flow after the change

- Direct-to-management entry points (`CirclesPanel` rows, `SearchBar` circle hit
  at `Explorer.tsx:882`, `RecommendationsView` at line 996, non-focused
  `MessageThread` at line 1054, `CirclesPanel` `onCreated` at line 953) → all call
  `openCircle` → `focusManagingIntent = true` + `focusCircleId = id` → `FocusMode`
  mounts with `managing = true` → lands directly on the management surface.
  **Regression avoided.**
- In-Focus navigation (T01 breadcrumb, `FocusSwitcher` in either header) → resets
  intent to `false` → but since `FocusMode` is already mounted, `managing` is
  preserved, so the user stays in whichever mode they were in.
- Exit → `focusCircleId = null` → `FocusMode` unmounts; next entry re-reads intent.

## Deviations from manifest projection

The manifest (`iterations → iter-04-drilldown-management-foldin`) *projected*:

- `sub-circle-drilldown` writing `FocusMode.tsx` + `FocusSwitcher.tsx`.
- `circle-management-foldin` writing `FocusManage.tsx` (new) + `CircleView.tsx` +
  `CircleSettings.tsx` + `Explorer.tsx`.

Projections are estimates. The finalized design is simpler and lower-risk:

- T01 touches only `FocusMode.tsx` (no `FocusSwitcher.tsx` edit — the breadcrumb
  is a separate affordance from existing props). The manifest constraint
  "exactly one writer of `FocusSwitcher.tsx`: switcher(i2), drilldown(i4)" is
  therefore not exercised in i4; not writing a file never breaks a
  write-collision constraint.
- T02 reuses `CircleView.tsx` verbatim (zero changes) and adds no
  `FocusManage.tsx`; it writes only `Explorer.tsx` + `FocusMode.tsx`. This keeps
  the manifest's core promise — "retire `CircleView.tsx` as a standalone screen"
  — while treating it as a reusable component rather than refactoring it.

## Verification (per task)

No web test script exists; QA is skipped (unchanged since iters 1-3). No web lint
is configured. Each task's completion gate, run in `web/`:

- `npx tsc -b` succeeds (no type errors).
- `npx vite build` succeeds.

Each task ends with `verification-before-completion` evidence and a per-task
commit.

## Known limitation (explicitly deferred)

`FocusMode.tsx` has NO mobile-responsive breakpoints — a fixed
`gridTemplateColumns` and no `md:` / `sm:` classes anywhere. T02 funnels MORE
entry points (circle chip taps, search results) into this non-responsive surface
instead of the old `CircleView`, which safely inherited Explorer's responsive
`<main>` wrapper. A full responsive redesign of a 4-panel dashboard is out of
scope for this already-large final iteration and was never part of the original
feature brainstorm. Recorded as a recommended follow-up; NOT fixed in T01 or T02.

## Assumptions

- [CORRECTION from adversarial review] The naive design would have silently
  regressed "click a circle → land on management" into "click a circle → land on
  the normal dashboard, management one extra click away." Fixed with an explicit
  `focusManagingIntent` / `initialManaging` flag so management-intent entry
  points (row clicks, search, recommendations, circle chips in ordinary chats)
  still land directly in Manage mode, while switcher / breadcrumb navigation
  (already-in-Focus-Mode circle switching) does not.
- [CONFIRMED via adversarial review] `CircleView.tsx` and `CircleSettings.tsx`
  need ZERO source changes — fully reusable as-is inside Focus Mode.
- [CONFIRMED via adversarial review] `parent_ids` / `child_circles` on the
  `Circle` type are real, populated fields (not aspirational), safe for T01 to
  rely on. (Verified: `api.ts:408-409`, both optional arrays — access guarded
  with `?.`.)
- [CORRECTION found while grounding] The `setSelectedCircle` removal list is
  larger than the brief stated: `setSelectedCircle(null)` also appears in
  `openTask` (line 362) and `openChatTasks` (line 372), beyond the named
  `openCircleTasks` / `openContactDM` / `openReco`. The plan enumerates all five,
  plus `closeMobileDetail` and `openCircle`, and requires a zero-match grep as
  the completion check.
- [KNOWN LIMITATION, explicitly deferred] `FocusMode.tsx` has no mobile-responsive
  treatment. Out of scope for this iteration; flagged as a recommended
  follow-up, not fixed here.

## Open questions

- Mobile responsiveness of Focus Mode (see "Known limitation" above) —
  recommended as a follow-up item, not blocking this iteration's completion.
- No other unresolved disagreements — every other concern raised by the
  adversarial review has a concrete fix folded into T02 above.

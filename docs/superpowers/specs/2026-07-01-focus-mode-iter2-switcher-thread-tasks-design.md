# Focus Mode — Iteration 2: Persistent Switcher, Inline Thread, Task Board (Design)

- Date: 2026-07-01
- Slug: focus-mode-iter2-switcher-thread-tasks
- Manifest: `docs/superpowers/turbo-manifests/i-need-to-develop-a-focus-mode-where-i-can-choose-manifest.yml`
- Iteration: iter-02-switcher-thread-tasks (2 of 4)
- Mode: turbo (parallel implementers, advisor review, unit tests skipped)

## Overview

Iteration 1 shipped the full-takeover Focus Mode shell (`FocusMode.tsx` = header +
"Exit Focus" + a 2-column grid of `FocusProfile.tsx` left / `FocusChatList.tsx` right),
a temporary per-circle-row "Focus" button in `CirclesPanel.tsx`, and Explorer's
`focusCircleId` state with an early-return takeover.

Iteration 2 turns that shell into the real product surface. It adds three things and
wires them into the existing shell without breaking it:

1. A persistent, reusable circle switcher that lets the user enter Focus Mode from
   anywhere (not only the Circles tab) and switch the focused circle without leaving
   Focus Mode.
2. An inline split-view chat thread: tapping a chat in the Focus chat list opens the
   full `MessageThread` next to the dashboard instead of exiting Focus Mode.
3. A circle-scoped task board, reusing the live `TasksView` component unchanged.

## Goal (this iteration)

- Add `FocusSwitcher.tsx`: a standalone control showing the active circle (or a neutral
  "Focus" label) that opens a dropdown of all circles; selecting one enters/switches.
- Render the switcher in two places: persistently near Explorer's tab bar (normal mode)
  and inside the Focus Mode header (switch focus without exiting).
- Remove iteration 1's temporary per-circle-row "Focus" button and its `onFocusCircle`
  prop, now superseded by the switcher.
- Add `FocusTasks.tsx`: a thin wrapper over the existing `TasksView` scoped to the focused
  circle, rendered in the Focus Mode left column beneath the profile.
- Add an inline `MessageThread` to the Focus Mode right column: row tap sets a local
  `activeChatJid` and renders the thread with a "Back to chats" control; the chat list
  shows again when no chat is active.

## Context (verified against the current codebase)

### Explorer (`web/src/explorer/Explorer.tsx`)

- `focusCircleId` / `setFocusCircleId` state: line 66.
- Early-return takeover (lines 542-555) renders `<FocusMode>` with props
  `circleId`, `circles`, `chats`, `nameMap`,
  `onOpenChat={(jid) => { setFocusCircleId(null); openChat(jid) }}` (the exit-wrapped
  navigation function iter 1's review added), and `onExit={() => setFocusCircleId(null)}`.
- `<CirclesPanel ... />` usage: lines 870-884, currently passes
  `onFocusCircle={setFocusCircleId}` (line 882).
- Reference `<MessageThread ... />` usage in normal mode: lines 963-986. It is the exact
  source for every prop the inline thread needs. Note it passes `onOpenChat={openChat}`
  (plain) in normal mode; the Focus inline thread must instead use the exit-wrapped
  version (see Design).
- `allTasks` state (line 74) is loaded by a `useEffect` (lines 180-186) that early-returns
  unless `tab === 'tasks'`, with deps `[tab, taskVersion]`. **This is the one real gap:**
  in Focus Mode the active `tab` is normally not `'tasks'`, so `allTasks` would be empty
  and the Focus task board would render nothing. Fix is in Design T04.
- Existing callbacks/state to thread through (all already present, no new logic to invent):
  `reloadCircles` (line 174), `reloadTags` (line 188), `bumpTasks` (line 226),
  `openTask` (line 339), `openChatTasks` (line 347), `openCircle` (line 509),
  `openChat` (line 228), `consumeChatDraft` (line 330), `chatDrafts` (line 115),
  `mentionIndex` (line 135), `selfDigits` (line 98), `tags`/`contactTags` state,
  `pendingJumpId` (line 88), `setPendingJumpId`, `liveMsg` (line 75),
  `device?.jid` for `ownJID`, and the `onSent` lambda
  `(m) => setChats((prev) => bumpChat(prev, m, selectedRef.current))` (line 985).

### Focus Mode (`web/src/explorer/FocusMode.tsx`)

- Current props (lines 9-23): `circleId`, `circles`, `chats`, `nameMap`, `onOpenChat`,
  `onExit`.
- Header (lines 28-42): color swatch + circle name + "Exit Focus".
- Content grid (lines 50-63): left column is one wrapper
  `<div className="min-h-0 overflow-y-auto rounded-lg border border-neutral-800">` around
  `<FocusProfile circleId circles nameMap />`; right column is a matching wrapper around
  `<FocusChatList circleId chats nameMap onOpenChat />`.

### Focus chat list (`web/src/explorer/FocusChatList.tsx`)

- Props (lines 11-21): `circleId`, `chats`, `nameMap`, `onOpenChat: (jid: string) => void`.
- Row tap calls `onOpenChat(chat.jid)` (line 55).
- Stale comment (lines 50-52) says the tap "exits Focus Mode into the normal full-screen
  thread" — no longer accurate after this iteration.

### Focus profile (`web/src/explorer/FocusProfile.tsx`)

- Root element (line 44) is `<div className="flex h-full flex-col overflow-y-auto">`.
  It works correctly once wrapped in its own `flex-1 overflow-hidden` box. **Do not edit
  this file** — it is not in this iteration's write set.

### Circles panel (`web/src/explorer/CirclesPanel.tsx`)

- `onFocusCircle: (id: number) => void` prop (line 27).
- Temporary per-row "Focus" button (lines 179-188) calling
  `e.stopPropagation(); onFocusCircle(c.id)`.

### Task board reuse (`web/src/explorer/TasksView.tsx`)

- Props (lines 19-39): `tasks`, `circles`, `chats`, `nameMap`, `ownJID`, `selection`,
  `onOpenTask`, `onCreated`, `onChanged`.
- When `selection.kind === 'circle'`, `filterByScope` (lines 404-406) filters
  `tasks.filter((t) => (t.circle_ids || []).includes(selection.id))` and
  `groupForRender` renders a single flat section. So passing the full task list plus
  `selection={{ kind: 'circle', id: circleId }}` scopes it correctly with **zero
  modification** to `TasksView`.
- `TasksView` has **no accept/reject review UI** — only a mark-done toggle (`toggleDone`,
  lines 84-88) and parent/child grouping. Any accept/reject UI lived only in
  `TasksPanel.tsx`, which is **dead code** (zero imports anywhere) and is not touched.
- `TasksSelection` type is exported from `web/src/explorer/TasksSidebar.tsx` (line 14):
  `{ kind: 'view'; view: QuickView } | { kind: 'circle'; id: number }`.

### Message thread (`web/src/explorer/MessageThread.tsx`)

- Props (lines 42-94) confirm every prop the inline thread needs already exists:
  `jid`, `chats`, `nameMap`, `mentionIndex`, `selfDigits`, `liveMsg`, `circles`,
  `allTags`, `contactTags`, `initialDraft`, `onDraftConsumed`, `onCirclesChanged`,
  `onTagsChanged`, `onOpenTask`, `onTasksChanged`, `onOpenChatTasks`, `onOpenChat`,
  `onOpenCircle`, `onSent`, `pendingJumpId`, `onJumpHandled`. `MessageThread` manages its
  own internal scroll and composer.

### API types (`web/src/api.ts`)

- `Chat` (line 148), `Message` (line 183), `Circle` (line 399), `Tag` (line 424),
  `Task` (line 434) are all exported. `api.getCircle` (line 1233) and `api.circleChats`
  (line 1252) exist. No backend (Go) change is needed anywhere in this iteration.

## Requirements

1. Add `FocusSwitcher.tsx`, a self-contained circle switcher (button + dropdown) that
   does not touch `Explorer.tsx` or `FocusMode.tsx`.
2. Render the switcher persistently near Explorer's tab bar so any circle can be focused
   from anywhere (not only the Circles tab).
3. Render the switcher in the Focus Mode header so the user can re-target the focused
   circle without leaving Focus Mode.
4. Remove iteration 1's temporary per-circle-row "Focus" button and its `onFocusCircle`
   prop / passed callback.
5. Add `FocusTasks.tsx`, a thin wrapper over `TasksView` scoped to the circle, rendered in
   the Focus Mode left column under the profile, each with its own internal scroll.
6. Add an inline `MessageThread` to the Focus Mode right column with a local `activeChatJid`
   and a "Back to chats" control; reset `activeChatJid` when the focused circle changes.
7. Every navigation callback threaded into `FocusMode` that should leave Focus Mode must
   first call `setFocusCircleId(null)`; the circle-chip callback must NOT (it re-targets).

## Design / Approach

Waves below use the plan's 0-indexed numbering (see the plan file). Human "Wave 0/1/2"
in the brief maps directly to plan waves 0/1/2.

### T01 — FocusSwitcher.tsx (new, wave 0)

Standalone, reusable component. Props: `circles: Circle[]`,
`activeCircleId: number | null`, `onSelect: (id: number) => void` (import `Circle` from
`../api`). Renders a compact button showing the active circle's color swatch + name, or a
neutral "Focus" label when `activeCircleId` is null. Clicking opens a dropdown/popover
listing all circles (color swatch + name); clicking one calls `onSelect(id)` and closes
the dropdown (with a click-away catcher, matching the app's existing menu pattern). No
imports of `Explorer.tsx` or `FocusMode.tsx`.

### T02 — FocusTasks.tsx (new, wave 0)

Thin wrapper over the live `TasksView`. Props: `circleId: number`, `tasks: Task[]`,
`circles: Circle[]`, `chats: Chat[]`, `nameMap: Map<string, string>`, `ownJID: string`,
`onOpenTask: (id: number) => void`, `onCreated: () => void`, `onChanged: () => void`
(types from `../api`, `TasksView` from `./TasksView`). Renders:

```
<TasksView
  selection={{ kind: 'circle', id: circleId }}
  tasks={tasks} circles={circles} chats={chats} nameMap={nameMap} ownJID={ownJID}
  onOpenTask={onOpenTask} onCreated={onCreated} onChanged={onChanged}
/>
```

`TasksPanel.tsx` is dead code and is not reused or touched. Ships with parent/child
grouping + mark-done only (no accept/reject — that never existed in `TasksView`).

### T03 — Wire the switcher in (wave 1, depends on T01)

- `Explorer.tsx`: render `<FocusSwitcher circles={circles} activeCircleId={null}
  onSelect={setFocusCircleId} />` as a persistent, always-visible control near the tab
  bar / sidebar header in normal (non-focused) mode, so any circle can be entered from
  anywhere. Import `FocusSwitcher`.
- `CirclesPanel.tsx`: remove the per-row "Focus" button (lines 179-188) and the
  `onFocusCircle` prop (line 27). In `Explorer.tsx`, remove `onFocusCircle={setFocusCircleId}`
  from the `<CirclesPanel ... />` usage (line 882).
- `FocusMode.tsx`: add prop `onSwitchCircle: (id: number) => void`; render
  `<FocusSwitcher circles={circles} activeCircleId={circleId} onSelect={onSwitchCircle} />`
  in the header next to the circle name/swatch. In `Explorer.tsx`, pass
  `onSwitchCircle={setFocusCircleId}` into `<FocusMode ... />` — calling the same setter
  with a different id re-targets Focus Mode without exiting.

### T04 — Wire tasks + inline thread (wave 2, depends on T02, T03)

This is the complex task; every fix below came from the adversarial design review that
already caught 5 real bugs in the naive version.

`Explorer.tsx`:

- **Required fix (allTasks freshness):** extend the `allTasks` load effect (lines 180-186)
  so it also loads while Focus Mode is active. Change the guard from
  `if (tab !== 'tasks') return` to `if (tab !== 'tasks' && focusCircleId == null) return`
  and add `focusCircleId` to the dep array (`[tab, taskVersion, focusCircleId]`). Without
  this, the Focus task board renders empty and never refreshes after create/mark-done,
  because `allTasks` only loads on the Tasks tab.
- Thread every new prop into the `<FocusMode ... />` usage from Explorer's existing
  state/callbacks: `mentionIndex`, `selfDigits`, `liveMsg`, `allTags={tags}`,
  `contactTags`, `allTasks`, `ownJID={device?.jid || ''}`, `pendingJumpId`,
  `onJumpHandled={() => setPendingJumpId(null)}`,
  `onSent={(m) => setChats((prev) => bumpChat(prev, m, selectedRef.current))}` (the same
  lambda the normal `MessageThread` already uses),
  `onCirclesChanged={reloadCircles}`, `onTagsChanged={reloadTags}`,
  `onTasksChanged={bumpTasks}`.
- Pass `chatDrafts` (the whole `Record<string,string>`) and `consumeChatDraft` down as two
  new `FocusMode` props (NOT `initialDraft`/`onDraftConsumed`), because `activeChatJid`
  lives inside `FocusMode`, not Explorer — so `FocusMode` computes
  `initialDraft={chatDrafts[activeChatJid || ''] || ''}` and
  `onDraftConsumed={() => activeChatJid && consumeChatDraft(activeChatJid)}` locally.
- **Required fix (exit-wrapping):** every navigation callback passed into `FocusMode` that
  should EXIT Focus Mode must call `setFocusCircleId(null)` first (same class of bug iter 1
  fixed — a callback silently mutating Explorer state while the early-return keeps rendering
  FocusMode):
  - `onOpenTask={(id) => { setFocusCircleId(null); openTask(id) }}`
  - `onOpenChatTasks={(jid) => { setFocusCircleId(null); openChatTasks(jid) }}`
  - `onOpenChat` — reuse iter 1's existing inline exit-wrapper on the `<FocusMode>` usage
    (`(jid) => { setFocusCircleId(null); openChat(jid) }`), forwarded by `FocusMode` to
    `MessageThread`'s `onOpenChat`. Do not build a second copy.
  - `onOpenCircle={(id) => setFocusCircleId(id)}` — **intentionally NOT exit-wrapped:**
    tapping a circle chip inside the focused thread re-targets Focus Mode to that circle
    (advisor-endorsed synergy with the switcher).

`FocusMode.tsx`:

- Add local state `const [activeChatJid, setActiveChatJid] = useState<string | null>(null)`.
- Add `useEffect(() => setActiveChatJid(null), [circleId])` — **required fix:** without it,
  switching circles via the switcher leaves a stale thread open for a chat that may not
  belong to the new circle.
- Restructure the LEFT column into a `flex flex-col` container with TWO child wrappers,
  each `min-h-0 flex-1 overflow-hidden` (each with its own internal scroll): one around
  `<FocusProfile circleId circles nameMap />` (unchanged props), one around
  `<FocusTasks circleId={circleId} tasks={allTasks} circles={circles} chats={chats}
  nameMap={nameMap} ownJID={ownJID} onOpenTask={onOpenTask} onCreated={onTasksChanged}
  onChanged={onTasksChanged} />`. **Required fix:** do NOT edit `FocusProfile.tsx`; the
  naive-version bug was stacking two `h-full`/`overflow` elements in a non-flex parent so
  the profile ate all the height — wrapping each in its own `flex-1 overflow-hidden` box
  fixes it.
- Restructure the RIGHT column: when `activeChatJid` is null, render
  `<FocusChatList circleId chats nameMap onSelectChat={setActiveChatJid} />` inside a
  wrapper with `overflow-y-auto` (as today). When `activeChatJid` is set, render
  `<MessageThread jid={activeChatJid} ... />` (all props above) plus a small
  "← Back to chats" button calling `setActiveChatJid(null)`, inside a wrapper WITHOUT
  `overflow-y-auto` — **required fix:** MessageThread manages its own scroll/composer;
  a parent `overflow-y-auto` on top causes double-scroll.
- Add all new props to `FocusMode`'s props type (`mentionIndex`, `selfDigits`, `liveMsg`,
  `allTags`, `contactTags`, `chatDrafts`, `consumeChatDraft`, `onCirclesChanged`,
  `onTagsChanged`, `onOpenTask`, `onTasksChanged`, `onOpenChatTasks`, `onOpenCircle`,
  `onSent`, `pendingJumpId`, `onJumpHandled`, `allTasks`, `ownJID`, plus `onSwitchCircle`
  from T03), importing types from `../api`.

`FocusChatList.tsx`:

- Replace the `onOpenChat: (jid: string) => void` prop with
  `onSelectChat: (jid: string) => void`; row tap calls `onSelectChat(chat.jid)`. Remove the
  stale comment (lines 50-52) about exiting Focus Mode.

## Non-goals (this iteration)

- Incremental circle digest, backend or UI (iter 3).
- Last-focused-circle persistence in localStorage (iter 3).
- Sub-circle drill-down / breadcrumb (iter 4).
- CircleView management fold-in / retiring `CircleView.tsx` (iter 4).
- Inline task-detail view (opening a task exits Focus Mode into the normal Tasks tab).
- Any backend (Go) change.
- Media-understanding surfacing (excluded from v1 by the manifest).
- Touching `TasksPanel.tsx` (dead code) or `FocusProfile.tsx`.

## Verification (this iteration)

- Correctness gate: `cd web && npx tsc -b` passes (the existing `typecheck` script). No
  test script and no lint config exist under `web/`.
- Build gate: `cd web && npx vite build` passes — this catches build-time errors `tsc -b`
  can miss and is a hard-won requirement from iter 1's plan review; it must not be weakened
  back to `tsc -b` alone. Every implementer task runs BOTH as its verification evidence.
- Manual demo (advisor / browse): the switcher enters and switches circles from anywhere;
  tapping a chat opens the thread inline with a working composer and "Back to chats"; the
  task board lists the circle's tasks and mark-done works; exit-navigation callbacks leave
  Focus Mode; the circle chip re-targets it.

## Assumptions

- [CORRECTION] `TasksPanel.tsx` confirmed dead code (zero imports) — not touched; the task
  board reuses the live `TasksView` via a thin wrapper instead.
- [CORRECTION] `TasksView` has no accept/reject review UI (only dead `TasksPanel.tsx` did)
  — task board ships with parent/child grouping + mark-done toggle only.
- [ASSUMPTION] Opening a task from the Focus Mode task board exits Focus Mode into the
  normal Tasks tab — no inline task-detail view this iteration, matching iter 1's
  chat-tap-exits pattern for consistency.
- [ASSUMPTION] Tapping a circle chip inside an inline-focused thread re-targets Focus Mode
  to that circle (reuses the switcher's `onSelect`) rather than exiting — advisor-endorsed
  synergy with the switcher work.
- [DECISION] Removed iter-1's temporary per-circle-row "Focus" button in `CirclesPanel.tsx`
  now that the persistent switcher supersedes it (iter 1's own spec flagged it as temporary).
- [PLANNER-ADDED, grounded in code] `allTasks` in Explorer only loads while
  `tab === 'tasks'` (lines 180-186); the Focus task board would otherwise be empty. T04
  extends that load-guard to also load while `focusCircleId != null`. This is the only
  logic (non-prop-threading) edit T04 adds to Explorer, and it stays inside Explorer's
  existing write set.

## Open questions

- None material — this iteration's design was reviewed once by an advisor pass that found
  5 concrete implementation bugs in the naive version (missing exit-wrapping on 3 callbacks,
  a missing `FocusChatList.tsx` in the write-set, a layout stacking bug, a double-scroll
  bug); all 5 are resolved inline above. The planner additionally grounded the design in the
  current code and surfaced one more real gap (the `allTasks` load-guard), resolved inside
  T04. No unresolved disagreements to ship with.

# Focus Mode — Iteration 2: Persistent Switcher, Inline Thread, Task Board (Plan)

- Date: 2026-07-01
- Spec: `docs/superpowers/specs/2026-07-01-focus-mode-iter2-switcher-thread-tasks-design.md`
- Format: file-ownership DAG (turbo). See
  `.claude/plugins/cache/shipgate/shipgate/0.7.0/skills/turbo/references/plan-format.md`.
- Execution: turbo subagent-driven, parallel write-only implementers per wave + a
  serialized committer per wave.

## Conventions

- Waves are 0-indexed per the plan-format reference: wave 0 is the independent wave. The
  brief's human-facing "Wave 0 / Wave 1 / Wave 2" map directly to plan waves 0 / 1 / 2.
- No TDD / unit-test tasks: turbo skips unit tests this iteration (no test script and no
  `*.test.*` / `*.spec.*` under `web/`, unchanged since iter 1).
- Lint is not configured (no eslint/biome under `web/`). The correctness gate is the
  existing `typecheck` script: `cd web && npx tsc -b`. Every implementer task ALSO runs
  `cd web && npx vite build` — a hard requirement from iter 1's plan review, not to be
  weakened back to `tsc -b` alone.
- Commit granularity: one commit per task (T03 and T04 are not tiny; do not collapse to
  wave-level).

## DAG summary

- Wave 0 (2 tasks, independent, no shared `files_write`): T01, T02.
- Wave 1 (1 task): T03, depends on T01.
- Wave 2 (1 task): T04, depends on T02, T03.
- wave_count = 3, max_wave_width = 2.

`FocusMode.tsx` and `Explorer.tsx` are each written by T03 (wave 1) and again by T04
(wave 2). This is safe: they are in different waves and T04 `depends_on: [T03]`, so T04's
implementer starts only after T03 is committed — there is no in-wave write-write collision.

## Tasks

```yaml
- id: T01
  description: Add FocusSwitcher.tsx — a standalone circle switcher (button + dropdown) driven by circles/activeCircleId/onSelect.
  files_write:
    - web/src/explorer/FocusSwitcher.tsx
  files_read:
    - web/src/api.ts
  wave: 0
  depends_on: []

- id: T02
  description: Add FocusTasks.tsx — a thin wrapper rendering the live TasksView scoped to selection={{kind:'circle', id: circleId}}.
  files_write:
    - web/src/explorer/FocusTasks.tsx
  files_read:
    - web/src/explorer/TasksView.tsx
    - web/src/explorer/TasksSidebar.tsx
  wave: 0
  depends_on: []

- id: T03
  description: Wire FocusSwitcher into Explorer (persistent) and FocusMode (header), and remove the temporary CirclesPanel per-row Focus button + onFocusCircle prop.
  files_write:
    - web/src/explorer/Explorer.tsx
    - web/src/explorer/FocusMode.tsx
    - web/src/explorer/CirclesPanel.tsx
  files_read:
    - web/src/explorer/FocusSwitcher.tsx
  wave: 1
  depends_on: [T01]

- id: T04
  description: Wire the circle task board and the inline split-view MessageThread into FocusMode, thread all props from Explorer, and fix the allTasks load-guard.
  files_write:
    - web/src/explorer/FocusMode.tsx
    - web/src/explorer/FocusChatList.tsx
    - web/src/explorer/Explorer.tsx
  files_read:
    - web/src/explorer/FocusTasks.tsx
    - web/src/explorer/MessageThread.tsx
    - web/src/explorer/TasksView.tsx
  wave: 2
  depends_on: [T02, T03]
```

## Task detail

### T01 — focus-entry-switcher component (wave 0)

- Create `web/src/explorer/FocusSwitcher.tsx`.
  - Props: `circles: Circle[]`, `activeCircleId: number | null`,
    `onSelect: (id: number) => void`. Import the `Circle` type from `../api`.
  - Render a compact trigger button: when `activeCircleId` is non-null, show that circle's
    color swatch + name (resolve via `circles.find((c) => c.id === activeCircleId)`); when
    null, show a neutral "Focus" label. Keep the dark-theme styling used elsewhere
    (`border-neutral-700`, `bg-neutral-800`, `text-neutral-300`).
  - Clicking the trigger toggles a dropdown/popover listing every circle (color swatch +
    name). Clicking a row calls `onSelect(circle.id)` and closes the dropdown. Add a
    click-away catcher (a fixed inset overlay) to dismiss on outside click — same pattern as
    `MoreMenu`/`DndButton` in `Explorer.tsx`.
  - Self-contained: do NOT import or edit `Explorer.tsx` or `FocusMode.tsx`.
- Verify: `cd web && npx tsc -b` passes AND `cd web && npx vite build` passes. Commit.

### T02 — focus-task-board wrapper (wave 0)

- Create `web/src/explorer/FocusTasks.tsx`.
  - Props: `circleId: number`, `tasks: Task[]`, `circles: Circle[]`, `chats: Chat[]`,
    `nameMap: Map<string, string>`, `ownJID: string`, `onOpenTask: (id: number) => void`,
    `onCreated: () => void`, `onChanged: () => void`. Import `Task`, `Circle`, `Chat` from
    `../api` and `TasksView` from `./TasksView`.
  - Render exactly:
    `<TasksView selection={{ kind: 'circle', id: circleId }} tasks={tasks} circles={circles}
    chats={chats} nameMap={nameMap} ownJID={ownJID} onOpenTask={onOpenTask}
    onCreated={onCreated} onChanged={onChanged} />`.
  - `TasksView` already filters to the circle when `selection.kind === 'circle'`
    (`filterByScope`, `TasksView.tsx:404-406`) — no change to `TasksView`. The
    `TasksSelection` type shape is defined in `TasksSidebar.tsx:14` (read-only reference;
    do not import it — inline the object literal).
  - Do NOT import, reuse, or touch `TasksPanel.tsx` (dead code).
- Verify: `cd web && npx tsc -b` passes AND `cd web && npx vite build` passes. Commit.

### T03 — wire the switcher in (wave 1, depends on T01)

- `web/src/explorer/Explorer.tsx`:
  - Add `import { FocusSwitcher } from './FocusSwitcher'`.
  - Render `<FocusSwitcher circles={circles} activeCircleId={null} onSelect={setFocusCircleId} />`
    as a persistent, always-visible control near the sidebar header / tab bar (the tab-button
    row is at lines 828-844) in normal (non-focused) mode, so any circle can be entered from
    anywhere — not only the Circles tab.
  - Remove `onFocusCircle={setFocusCircleId}` from the `<CirclesPanel ... />` usage (line 882).
  - In the early-return `<FocusMode ... />` (lines 542-555), add
    `onSwitchCircle={setFocusCircleId}` (re-targets Focus Mode without exiting).
- `web/src/explorer/CirclesPanel.tsx`:
  - Remove the `onFocusCircle: (id: number) => void` prop (line 27) from the props type and
    destructuring.
  - Remove the per-row "Focus" button (lines 179-188).
- `web/src/explorer/FocusMode.tsx`:
  - Add prop `onSwitchCircle: (id: number) => void` to the props type.
  - Add `import { FocusSwitcher } from './FocusSwitcher'`.
  - In the header (near the circle name/swatch, lines 28-42), render
    `<FocusSwitcher circles={circles} activeCircleId={circleId} onSelect={onSwitchCircle} />`.
  - Do NOT add the inline-thread / task-board props here — that is T04. Keep the existing
    two-column body untouched in this task.
- Verify: `cd web && npx tsc -b` passes AND `cd web && npx vite build` passes. Commit.

### T04 — wire tasks + inline thread (wave 2, depends on T02, T03)

Follow every "required fix" precisely — they came from the adversarial design review plus
the planner's code-grounding pass.

- `web/src/explorer/Explorer.tsx`:
  - **Required fix (allTasks freshness):** in the `allTasks` load effect (lines 180-186),
    change the guard `if (tab !== 'tasks') return` to
    `if (tab !== 'tasks' && focusCircleId == null) return`, and add `focusCircleId` to the
    dep array so it becomes `[tab, taskVersion, focusCircleId]`. Without this the Focus task
    board is empty and never refreshes.
  - Thread every new prop into the `<FocusMode ... />` usage (lines 542-555), sourced from
    Explorer's existing state/callbacks (prop-threading only):
    `mentionIndex`, `selfDigits`, `liveMsg`, `allTags={tags}`, `contactTags`,
    `allTasks={allTasks}`, `ownJID={device?.jid || ''}`, `pendingJumpId`,
    `onJumpHandled={() => setPendingJumpId(null)}`,
    `onSent={(m) => setChats((prev) => bumpChat(prev, m, selectedRef.current))}` (the exact
    lambda the normal `MessageThread` uses at line 985),
    `onCirclesChanged={reloadCircles}`, `onTagsChanged={reloadTags}`,
    `onTasksChanged={bumpTasks}`.
  - Pass `chatDrafts={chatDrafts}` and `consumeChatDraft={consumeChatDraft}` as two new
    `FocusMode` props (NOT `initialDraft` / `onDraftConsumed`), because `activeChatJid` lives
    inside `FocusMode`; `FocusMode` computes the draft locally.
  - **Required fix (exit-wrapping):** wrap exit-navigation callbacks on the `<FocusMode>`
    usage so they call `setFocusCircleId(null)` first:
    - `onOpenTask={(id) => { setFocusCircleId(null); openTask(id) }}`
    - `onOpenChatTasks={(jid) => { setFocusCircleId(null); openChatTasks(jid) }}`
    - `onOpenChat` — keep iter 1's existing inline exit-wrapper already on this usage
      (`(jid) => { setFocusCircleId(null); openChat(jid) }`); `FocusMode` forwards it to
      `MessageThread`. Do not create a second copy.
    - `onOpenCircle={(id) => setFocusCircleId(id)}` — **intentionally NOT exit-wrapped**
      (re-targets Focus Mode to the tapped circle chip).
- `web/src/explorer/FocusMode.tsx`:
  - Add `useState`/`useEffect` imports. Add
    `const [activeChatJid, setActiveChatJid] = useState<string | null>(null)`.
  - Add `useEffect(() => setActiveChatJid(null), [circleId])` — **required fix**: clears a
    stale thread when the focused circle changes via the switcher.
  - Add all new props to the props type: `mentionIndex`, `selfDigits`, `liveMsg`, `allTags`,
    `contactTags`, `chatDrafts` (`Record<string, string>`), `consumeChatDraft`
    (`(jid: string) => void`), `onCirclesChanged`, `onTagsChanged`, `onOpenTask`,
    `onTasksChanged`, `onOpenChatTasks`, `onOpenCircle`, `onSent`, `pendingJumpId`,
    `onJumpHandled`, `allTasks` (`Task[]`), `ownJID` (`string`) — plus `onSwitchCircle`
    from T03. Import the added types `Message`, `Tag`, `Task` from `../api` and the
    `MentionEntry` type from `./format`; import `MessageThread` from `./MessageThread` and
    `FocusTasks` from `./FocusTasks`.
  - Restructure the LEFT column (currently lines 54-57) into a `flex flex-col` container
    with TWO child wrappers, each `min-h-0 flex-1 overflow-hidden`, each with its own
    internal scroll: one around `<FocusProfile circleId={circleId} circles={circles}
    nameMap={nameMap} />` (unchanged), one around
    `<FocusTasks circleId={circleId} tasks={allTasks} circles={circles} chats={chats}
    nameMap={nameMap} ownJID={ownJID} onOpenTask={onOpenTask} onCreated={onTasksChanged}
    onChanged={onTasksChanged} />`. **Required fix:** do NOT edit `FocusProfile.tsx`; wrap
    each panel in its own `flex-1 overflow-hidden` box (the naive bug was two `h-full`
    panels in a non-flex parent, so the profile ate all the height).
  - Restructure the RIGHT column (currently lines 59-62): when `activeChatJid` is null,
    render `<FocusChatList circleId={circleId} chats={chats} nameMap={nameMap}
    onSelectChat={setActiveChatJid} />` inside a wrapper with `overflow-y-auto` (as today).
    When `activeChatJid` is set, render a "← Back to chats" button (calls
    `setActiveChatJid(null)`) plus
    `<MessageThread jid={activeChatJid} chats={chats} nameMap={nameMap}
    mentionIndex={mentionIndex} selfDigits={selfDigits} liveMsg={liveMsg} circles={circles}
    allTags={allTags} contactTags={contactTags}
    initialDraft={chatDrafts[activeChatJid] || ''}
    onDraftConsumed={() => consumeChatDraft(activeChatJid)} onCirclesChanged={onCirclesChanged}
    onTagsChanged={onTagsChanged} onOpenTask={onOpenTask} onTasksChanged={onTasksChanged}
    onOpenChatTasks={onOpenChatTasks} onOpenChat={onOpenChat} onOpenCircle={onOpenCircle}
    onSent={onSent} pendingJumpId={pendingJumpId} onJumpHandled={onJumpHandled} />` inside a
    wrapper WITHOUT `overflow-y-auto` — **required fix:** MessageThread owns its scroll and
    composer; a parent `overflow-y-auto` causes double-scroll.
- `web/src/explorer/FocusChatList.tsx`:
  - Replace the `onOpenChat: (jid: string) => void` prop (lines 14-20) with
    `onSelectChat: (jid: string) => void`; change the row `onClick` (line 55) to
    `onSelectChat(chat.jid)`. Remove the stale "exits Focus Mode" comment (lines 50-52).
- Verify: `cd web && npx tsc -b` passes AND `cd web && npx vite build` passes. Commit.

## Self-validation

1. No write-write collision within a wave:
   - Wave 0 `files_write` union: `FocusSwitcher.tsx` (T01); `FocusTasks.tsx` (T02). Distinct.
     PASS.
   - Wave 1: only T03 (`Explorer.tsx`, `FocusMode.tsx`, `CirclesPanel.tsx`). PASS.
   - Wave 2: only T04 (`FocusMode.tsx`, `FocusChatList.tsx`, `Explorer.tsx`). PASS.
   - `FocusMode.tsx` and `Explorer.tsx` are written by T03 (wave 1) and T04 (wave 2) —
     different waves, and T04 `depends_on: [T03]`, so no in-wave collision. PASS.
2. Wave monotonicity: T03 (wave 1) depends on T01 (wave 0); 1 > 0. T04 (wave 2) depends on
   T02 (wave 0) and T03 (wave 1); 2 > 0 and 2 > 1. Wave-0 tasks (T01, T02) have empty
   `depends_on`. PASS.
3. No placeholders in file paths: every `files_write` / `files_read` entry is a concrete
   path; no globs, no TBD. PASS.

DAG validation: PASS.

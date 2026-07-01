# Focus Mode — Iteration 1: Takeover Shell + First Dashboard Blocks (Plan)

- Date: 2026-07-01
- Spec: `docs/superpowers/specs/2026-07-01-focus-mode-iter1-shell-foundation-design.md`
- Format: file-ownership DAG (turbo). See
  `.claude/plugins/cache/shipgate/shipgate/0.7.0/skills/turbo/references/plan-format.md`.
- Execution: turbo subagent-driven, parallel write-only implementers per wave + a
  serialized committer per wave.

## Conventions

- Waves are 0-indexed per the plan-format reference: wave 0 is the independent wave.
  (The brainstorm's human-facing "Wave 1 / Wave 2" map to plan waves 0 / 1.)
- No TDD / unit-test tasks: turbo skips unit tests this iteration (no test script and no
  `*.test.*` / `*.spec.*` under `web/`).
- Lint is not configured (no eslint/biome under `web/`). The correctness gate is the
  existing `typecheck` script: `cd web && npx tsc -b`. Every implementer task runs it as
  its own verification evidence before committing.
- Commit granularity: one commit per task (T02 and T04 are not tiny; do not collapse to
  wave-level).

## DAG summary

- Wave 0 (4 tasks, all independent, no shared `files_write`): T01, T02, T03, T04.
- Wave 1 (1 task): T05, depends on T02, T03, T04.
- wave_count = 2, max_wave_width = 4.

## Tasks

```yaml
- id: T01
  description: Rename the WorkingHours "(focus mode)" auto-mute label to "(quiet hours)".
  files_write: [web/src/explorer/WorkingHours.tsx]
  files_read: []
  wave: 0
  depends_on: []

- id: T02
  description: Add Focus Mode takeover shell (Explorer state + early return + FocusMode.tsx) and a temporary per-circle Focus entry button in CirclesPanel.
  files_write:
    - web/src/explorer/Explorer.tsx
    - web/src/explorer/FocusMode.tsx
    - web/src/explorer/CirclesPanel.tsx
  files_read:
    - web/src/api.ts
  wave: 0
  depends_on: []

- id: T03
  description: Add api.circleChats client wrapper and FocusChatList.tsx (filtered circle chat list with its own minimal row).
  files_write:
    - web/src/api.ts
    - web/src/explorer/FocusChatList.tsx
  files_read:
    - web/src/explorer/ChatList.tsx
  wave: 0
  depends_on: []

- id: T04
  description: Add FocusProfile.tsx (reuse ProfileCard for purpose + read-only circle member list).
  files_write:
    - web/src/explorer/FocusProfile.tsx
  files_read:
    - web/src/explorer/CircleView.tsx
  wave: 0
  depends_on: []

- id: T05
  description: Wire FocusProfile and FocusChatList into FocusMode's two placeholder slots.
  files_write:
    - web/src/explorer/FocusMode.tsx
  files_read:
    - web/src/explorer/FocusProfile.tsx
    - web/src/explorer/FocusChatList.tsx
  wave: 1
  depends_on: [T02, T03, T04]
```

## Task detail

### T01 — naming-collision-fix (wave 0)

- Edit `web/src/explorer/WorkingHours.tsx` (~line 167). Change the text
  "Selected chats are muted during working hours (focus mode)." to
  "Selected chats are muted during working hours (quiet hours)." Text-only change; no
  logic touched.
- Verify: `cd web && npx tsc -b` passes. Commit.

### T02 — focus-mode-shell (wave 0)

- In `web/src/explorer/Explorer.tsx`:
  - Add state: `const [focusCircleId, setFocusCircleId] = useState<number | null>(null)`.
  - At the very top of the component's `return` (before the existing
    `<div className="flex h-screen overflow-hidden bg-neutral-950 ...">` at line 537),
    add an early return: `if (focusCircleId != null) return <FocusMode circleId={focusCircleId}
    circles={circles} chats={chats} nameMap={nameMap} onOpenChat={openChat}
    onExit={() => setFocusCircleId(null)} />`. This is the full takeover — tab bar, aside,
    and main are not rendered while focused.
  - Pass `onFocusCircle={setFocusCircleId}` to the existing `<CirclesPanel ... />` usage
    (line 851).
  - Add the `import { FocusMode } from './FocusMode'`.
- Create `web/src/explorer/FocusMode.tsx`:
  - Props: `circleId: number`, `circles: Circle[]`, `chats: Chat[]`,
    `nameMap: Map<string, string>`, `onOpenChat: (jid: string) => void`, `onExit: () => void`.
    Import the `Circle` and `Chat` types from `../api`.
  - Render a header: circle name + color swatch (resolve the circle via
    `circles.find((c) => c.id === circleId)`; use `circle.color` for the swatch) plus an
    "Exit Focus" button calling `onExit`.
  - Render a content area with a layout (CSS grid with named areas, or flex) able to take
    more panels later. Include two clearly-labeled placeholder slots — one for the profile
    panel, one for the chat-list panel — marked so T05 can find and replace them (e.g. a
    labeled `<div>` with a `{/* profile slot */}` / `{/* chat-list slot */}` comment). Do
    not build task-board or digest panels now.
- In `web/src/explorer/CirclesPanel.tsx`:
  - Add `onFocusCircle: (id: number) => void` to the props type.
  - Add a small "Focus" button to each circle row (inside `renderNode`, near the existing
    per-row `+` new-sub-circle button around lines 161-176). On click, call
    `e.stopPropagation()` then `onFocusCircle(c.id)`.
- Verify: `cd web && npx tsc -b` passes. Commit.
- Note: FocusMode's two slots stay as placeholders after this task; T05 fills them. Do
  not import FocusProfile / FocusChatList here (they may not exist yet in a parallel wave).

### T03 — focus-chat-list-panel (wave 0)

- In `web/src/api.ts`, add to the `api` object (copy the `circleContacts` wrapper shape at
  line 1245):
  `circleChats: async (id: number): Promise<string[]> => { const res = await
  fetch(\`/api/v2/circles/${id}/chats\`); const data = await res.json(); return
  data.chat_jids ?? [] }`. This calls the existing `GET /api/v2/circles/{id}/chats`
  (`handleCircleChats`, `internal/api/handler_circles.go:375`) — no backend change.
- Create `web/src/explorer/FocusChatList.tsx`:
  - Props: `circleId: number`, `chats: Chat[]`, `nameMap: Map<string, string>`,
    `onOpenChat: (jid: string) => void`. Import `Chat` from `../api` and `api`.
  - On mount / when `circleId` changes, fetch `api.circleChats(circleId)` into a JID
    `Set<string>`.
  - Filter the `chats` prop to those JIDs; render its OWN minimal row per chat:
    avatar/initials, display name via `nameMap.get(chat.jid)` (fall back to `chat.name`),
    unread badge from `unread_count` / `unread_mentions`, muted icon from `is_muted`.
  - Do NOT import or modify `ChatList.tsx` (read-only reference for row shape only).
  - Row tap calls `onOpenChat(chat.jid)`. Add a one-line comment: inline split-view thread
    rendering lands in a later iteration; for now this exits Focus Mode into the normal
    full-screen thread (existing `onOpenChat` behavior).
- Verify: `cd web && npx tsc -b` passes. Commit.

### T04 — focus-profile-panel (wave 0)

- Create `web/src/explorer/FocusProfile.tsx`:
  - Props: `circleId: number`, `circles: Circle[]`, `nameMap: Map<string, string>`.
    Import `ProfileCard` from `./ProfileCard`, `api` + `Circle` + `CircleMember` from
    `../api`, and `jidUser` from `./format` (same helper `CircleView.tsx` uses).
  - Render `<ProfileCard type="circle" ref_={String(circleId)} />` for the purpose section
    (same component used in `CircleView.tsx:349`; do not rebuild profile-fetch/edit logic).
  - Below it, render a read-only member list: fetch `api.getCircle(circleId)` for
    `detail.members`, and resolve each member's label exactly like `memberLabel` in
    `CircleView.tsx:101-106` — circle members via
    `circles.find((c) => String(c.id) === m.member_ref)?.name || \`Circle ${m.member_ref}\``,
    everything else via `nameMap.get(m.member_ref) || '+' + jidUser(m.member_ref)`.
  - No add/remove/expand controls (that stays in CircleView.tsx). No tags section
    (circles do not support the tag-chip system).
- Verify: `cd web && npx tsc -b` passes. Commit.

### T05 — focus-mode-integration (wave 1, depends on T02, T03, T04)

- Edit `web/src/explorer/FocusMode.tsx` (created in T02):
  - Import `{ FocusProfile }` from `./FocusProfile` and `{ FocusChatList }` from
    `./FocusChatList`.
  - Replace the profile placeholder slot with
    `<FocusProfile circleId={circleId} circles={circles} nameMap={nameMap} />`.
  - Replace the chat-list placeholder slot with
    `<FocusChatList circleId={circleId} chats={chats} nameMap={nameMap} onOpenChat={onOpenChat} />`.
  - This is the only task that edits FocusMode.tsx after its creation — strictly after T02.
- Verify: `cd web && npx tsc -b` passes. Commit.

## Self-validation

1. No write-write collision within a wave:
   - Wave 0 `files_write` union: `WorkingHours.tsx` (T01); `Explorer.tsx`, `FocusMode.tsx`,
     `CirclesPanel.tsx` (T02); `api.ts`, `FocusChatList.tsx` (T03); `FocusProfile.tsx`
     (T04). All distinct — no path appears twice. PASS.
   - Wave 1: only T05 (`FocusMode.tsx`). PASS.
   - `FocusMode.tsx` is written by T02 (wave 0) and T05 (wave 1) — different waves, so no
     in-wave collision. PASS.
2. Wave monotonicity: T05 (wave 1) depends on T02, T03, T04 (all wave 0); 1 > 0. All wave-0
   tasks have empty `depends_on`. PASS.
3. No placeholders in file paths: every `files_write` / `files_read` entry is a concrete
   path; no globs, no TBD. PASS.

DAG validation: PASS.

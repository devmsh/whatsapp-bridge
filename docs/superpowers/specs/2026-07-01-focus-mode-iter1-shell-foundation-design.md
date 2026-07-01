# Focus Mode — Iteration 1: Takeover Shell + First Dashboard Blocks (Design)

- Date: 2026-07-01
- Slug: focus-mode-iter1-shell-foundation
- Manifest: `docs/superpowers/turbo-manifests/i-need-to-develop-a-focus-mode-where-i-can-choose-manifest.yml`
- Iteration: iter-01-focus-shell-foundation (1 of 4)
- Mode: turbo (parallel implementers, advisor review, unit tests skipped)

## Overview

"Focus Mode" is a new per-circle, full-screen dashboard. The user picks a circle and
sees a redesigned home surface built around that circle, instead of the normal chat
list. This iteration ships the first demoable version: a full-takeover shell plus two
low-risk dashboard panels (a filtered chat list and a circle profile), reachable through
a temporary entry button.

This iteration deliberately does NOT ship: the persistent top-level switcher, inline
split-view thread rendering, the task board, the incremental digest engine, last-circle
persistence, sub-circle drill-down, or the CircleView management fold-in. Those land in
iterations 2 through 4 and are out of scope here.

## Goal (this iteration)

Ship a demoable, full-takeover Focus Mode for one circle with:

- A full-screen shell that replaces the normal tab-bar UI while active, with an
  "Exit Focus" control that returns to the normal UI.
- A temporary entry point: a small "Focus" button on each circle row.
- Two working dashboard panels: a filtered chat list (this circle's flattened chats)
  and a circle profile (purpose text plus a read-only member list).
- A naming-collision fix so "Focus Mode" refers only to this new feature.

## Context (verified against the codebase)

- Entry component: `web/src/explorer/Explorer.tsx` (~1267 lines). Its `return` starts at
  line 537 with `<div className="flex h-screen overflow-hidden bg-neutral-950 ...">`.
  `CirclesPanel` is rendered at line 851 (no `onFocusCircle` prop today). `openChat`
  is a `useCallback` with signature `(jid: string, draft?: string) => void` (line 223).
  `nameMap` is `Map<string, string>` from `buildNameMap(contacts, groups)`
  (`web/src/explorer/format.ts:44`). `chats` is `Chat[]` state and `circles` is `Circle[]`.
- `Circle` (`web/src/api.ts:399`): `{ id, name, color, member_count, child_circles?, ... }`.
- `Chat` (`web/src/api.ts:148`): `{ jid, name, unread_count, is_muted, unread_mentions?, ... }`.
- `CircleDetail` (`web/src/api.ts:747`): `{ circle, members: CircleMember[] }`, where
  `CircleMember` is `{ circle_id, member_type, member_ref, added_at }`.
- `api.getCircle(id)` already exists (`web/src/api.ts:1233`) and returns `CircleDetail`.
  The GET-wrapper pattern to copy is `circleContacts` (`web/src/api.ts:1245`):
  `async (id) => { const res = await fetch(...); return res.json() }`.
- Circle-chats endpoint already exists: `GET /api/v2/circles/{id}/chats` →
  `handleCircleChats` (`internal/api/handler_circles.go:375`, routed via `case "chats":`
  at line 211), which calls `s.store.FlattenCircleChats(id)` and returns
  `{"chat_jids": string[]}`. No backend change is needed anywhere in this iteration.
- `ProfileCard` (`web/src/explorer/ProfileCard.tsx`, imported from `./ProfileCard`) is
  used in `CircleView.tsx:349` as `<ProfileCard type="circle" ref_={String(circleId)} defaultOpen />`.
- Member-name resolution pattern to copy is `memberLabel` in `CircleView.tsx:101-106`:
  circle members resolve via `circles.find((c) => String(c.id) === m.member_ref)?.name`,
  everything else via `nameMap.get(m.member_ref) || '+' + jidUser(m.member_ref)`
  (`jidUser` is imported in `CircleView.tsx`).
- Naming collision: `web/src/explorer/WorkingHours.tsx:167` reads
  "Selected chats are muted during working hours (focus mode)."

## Requirements

1. Rename the WorkingHours "(focus mode)" parenthetical to "(quiet hours)" so "Focus
   Mode" is unambiguous.
2. Add a full-takeover Focus Mode: when a circle is focused, Explorer renders only the
   Focus Mode screen (tab bar, aside, and main are not rendered).
3. Provide a temporary entry point: a "Focus" button on each circle row in
   `CirclesPanel.tsx`, wired up to Explorer state.
4. FocusMode shows a header (circle name + color swatch + "Exit Focus") and a content
   area with two panels: profile and chat list. The layout must accept more panels
   later (task board, digest) without a rewrite.
5. The chat-list panel shows only the circle's flattened chats, with unread count,
   mention badge, and muted state. Tapping a chat calls `onOpenChat` (which, this
   iteration, exits Focus Mode and opens the normal full-screen thread).
6. The profile panel shows the circle purpose (reusing `ProfileCard`) and a read-only
   member list. No add/remove/expand controls; no tags section.

## Design / Approach

### Naming fix (T01)
Change the WorkingHours label text only. Lowest-risk, no logic change.

### Shell + entry (T02)
Add `focusCircleId` state to Explorer. At the top of Explorer's `return`, before the
main `<div className="flex h-screen ...">`, add an early return: when `focusCircleId != null`,
render `<FocusMode circleId={focusCircleId} circles={circles} chats={chats} nameMap={nameMap}
onOpenChat={openChat} onExit={() => setFocusCircleId(null)} />`. Create `FocusMode.tsx`
with the header and two clearly-labeled placeholder slots (profile slot, chat-list slot)
for wiring in T05. Use a layout (CSS grid with named areas, or flex) that can take more
panels later. Add an `onFocusCircle: (id: number) => void` prop to `CirclesPanel` and a
per-row "Focus" button; pass `onFocusCircle={setFocusCircleId}` at the `CirclesPanel`
usage in Explorer (line 851).

### Chat-list panel (T03)
Add `api.circleChats(id): Promise<string[]>` that calls `GET /api/v2/circles/{id}/chats`
and unwraps `{chat_jids}`. Create `FocusChatList.tsx` that fetches the JID set, filters
the `chats: Chat[]` prop to those JIDs, and renders its own minimal row per chat
(avatar/initials, `nameMap` name, unread badge from `unread_count`/`unread_mentions`,
muted icon from `is_muted`). It does not import or edit `ChatList.tsx` (that row closes
over too much local state — drafts, typing, context menus — to reuse safely). A row tap
calls `onOpenChat`. A one-line comment notes that inline split-view thread rendering
lands in a later iteration.

### Profile panel (T04)
Create `FocusProfile.tsx` rendering `<ProfileCard type="circle" ref_={String(circleId)} />`
for the purpose section, then a read-only member list. Fetch `api.getCircle(circleId)`
for `detail.members` and resolve each member's display name exactly like `memberLabel`
in `CircleView.tsx:101-106` (so `nameMap` and `circles` are props). No add/remove/expand
controls. No tags section.

### Integration (T05)
Edit `FocusMode.tsx` (created in T02) to replace the two placeholder slots with real
`<FocusProfile .../>` and `<FocusChatList .../>` renders. This is the only task that
touches `FocusMode.tsx` after creation, so it is sequenced strictly after T02.

## Non-goals (this iteration)

- Persistent top-level switcher (iter 2).
- Inline split-view thread rendering (iter 2).
- Task board (iter 2).
- Incremental circle digest, backend or UI (iter 3).
- Last-focused-circle persistence (iter 3).
- Sub-circle drill-down / breadcrumb (iter 4).
- CircleView management fold-in / retiring CircleView.tsx (iter 4).
- Any backend (Go) change.
- Media-understanding surfacing (excluded from v1 by the manifest).

## Verification (this iteration)

- Correctness gate: `cd web && npx tsc -b` passes (no test script and no lint config
  exist under `web/`; `tsc -b` is the de-facto gate). Each implementer task runs it as
  its own verification evidence.
- Manual demo: click a circle's "Focus" button → full-screen Focus Mode opens with the
  circle name/color, a member list + purpose, and a filtered chat list; "Exit Focus"
  returns to the normal UI.

## Assumptions

- [ASSUMPTION] Renamed WorkingHours.tsx wording to "quiet hours" — exact replacement
  text is a low-stakes guess, easy to adjust later if the user prefers different wording.
- [ASSUMPTION] Temporary iter-1 entry point = a small Focus icon per circle row in
  CirclesPanel.tsx; fully superseded by iter 2's persistent top-level switcher.
- [ASSUMPTION] Tapping a chat in iter 1's FocusChatList exits Focus Mode and opens the
  normal full-screen thread view (temporary — true inline split-view thread lands in
  iter 2's focus-inline-thread-view feature).
- [ASSUMPTION/CORRECTION] Profile panel ships with purpose (via existing ProfileCard) +
  read-only member list only, NO tags section — circles don't support the tag-chip
  system DashboardModal uses for contacts/groups; an earlier brainstorming pass conflated
  the two, corrected during advisor review.
- [CONFIRMED via codebase scan + advisor review] No backend (Go) changes needed anywhere
  in this iteration.
- [CONFIRMED via advisor review] FocusChatList renders its own minimal row rather than
  reusing ChatList.tsx's row (too much closed-over local state, zero test coverage) —
  removes ChatList.tsx from the write-DAG entirely.

## Open questions

- None material — iter 1 was reviewed once by an advisor pass and all concerns were
  resolved inline (see Assumptions). No unresolved disagreements to ship with.

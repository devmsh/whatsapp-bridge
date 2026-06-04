# Working Hours Auto-Mute — Implementation Plan (file-ownership DAG)

Spec: `docs/superpowers/specs/2026-06-04-working-hours-auto-mute-design.md`

Execution: turbo iteration Workflow. Implementers are write-only; a serialized
committer commits each wave. Backend implementers verify by building **only their
own package** (e.g. `go build ./internal/wa/`) — never `go build ./...`, which
would race other half-written files in the same parallel wave. Frontend verifies
with `cd web && npx tsc --noEmit`. The whole-module `go build ./... && go vet ./...`
runs once in the Review phase.

## Waves

```yaml
- id: T01
  description: Create internal/wa/working_hours.go. WorkingHoursConfig type (Enabled, Start, End, WorkingDays []int, ChatJIDs, FeatureMuted []string). LoadWorkingHoursConfig(store) with defaults (disabled, 09:00-18:00, days [0,1,2,3,4]). SaveWorkingHoursConfig(store,cfg). Pure DesiredMute(now,cfg) bool — half-open [start,end), returns false if !Enabled, if weekday not in WorkingDays, or if Start>=End (invalid; no overnight). windowEndUnix(now,cfg) for muted_until. Reconcile(client,store,now), ReconcileNow(client,store), ReleaseMutes(client,store,jids) — unmute jids + drop from feature_muted. ALL reconcile funcs serialized behind a package-level sync.Mutex. Mute via appstate.BuildMute(jid,true,-1)+SetChatMuted(jid,true,endUnix); unmute via BuildMute(jid,false,0)+SetChatMuted(jid,false,0). Honor feature_muted ownership rule. Skip (no error) when !Enabled or !client.IsConnected(). StartWorkingHoursScheduler(client,store) — 1-min ticker + one startup run. No HTTP, no UI. Verify with `go build ./internal/wa/` and `go vet ./internal/wa/`.
  files_write: [internal/wa/working_hours.go]
  files_read: [internal/wa/sync.go, internal/wa/client.go, internal/db/sync_state.go, internal/db/chats.go, internal/api/handler_chats.go]
  wave: 0
  depends_on: []

- id: T02
  description: Modify web/src/api.ts — add WorkingHoursConfig type (enabled, start, end, working_days:number[], chat_jids:string[], feature_muted:string[]) and api.workingHours() GET /api/v2/working-hours + api.setWorkingHours(cfg) PUT, following the mediaSettings/setMediaSettings patterns already in the file.
  files_write: [web/src/api.ts]
  files_read: []
  wave: 0
  depends_on: []

- id: T03
  description: Create internal/wa/working_hours_test.go — table-driven tests for DesiredMute covering inside window, before start, after end, exactly-at-start (muted), exactly-at-end (unmuted, half-open), weekend day (unmuted), invalid window Start>=End (unmuted), and disabled (unmuted). Use fixed time.Date values in the host local zone; no real clock. Run `go test ./internal/wa/`.
  files_write: [internal/wa/working_hours_test.go]
  files_read: [internal/wa/working_hours.go]
  wave: 1
  depends_on: [T01]

- id: T04
  description: Create internal/api/handler_working_hours.go. GET returns wa.LoadWorkingHoursConfig(store) as JSON. PUT decodes enabled/start/end/working_days/chat_jids; reject 400 if start/end not HH:MM or start>=end. Order: (1) load old cfg; (2) removed = (old.ChatJIDs ∩ old.FeatureMuted) − new.ChatJIDs, if non-empty wa.ReleaseMutes(client,store,removed); (3) if new Enabled==false, wa.ReleaseMutes(client,store,old.FeatureMuted); (4) SaveWorkingHoursConfig with trimmed feature_muted (client never sets feature_muted); (5) wa.ReconcileNow(client,store); (6) return saved config. Follow handler_chats.go conventions (decodeJSON, jsonOK, jsonError, methodNotAllowed). Verify with `go build ./internal/api/`.
  files_write: [internal/api/handler_working_hours.go]
  files_read: [internal/wa/working_hours.go, internal/api/handler_chats.go, internal/api/server.go]
  wave: 1
  depends_on: [T01]

- id: T05
  description: Create web/src/explorer/WorkingHours.tsx — settings modal (enable toggle, start/end time inputs, Sun–Sat working-day checkboxes, chat picker for chat_jids). Load via api.workingHours(), save via api.setWorkingHours(). Follow PrivacySettings.tsx structure/styling. Export a WorkingHours({onClose}) component.
  files_write: [web/src/explorer/WorkingHours.tsx]
  files_read: [web/src/api.ts, web/src/explorer/PrivacySettings.tsx, web/src/Settings.tsx]
  wave: 1
  depends_on: [T02]

- id: T07
  description: Modify main.go — call wa.StartWorkingHoursScheduler(client, store) right after the StartPeriodicSync call.
  files_write: [main.go]
  files_read: [internal/wa/working_hours.go]
  wave: 1
  depends_on: [T01]

- id: T06
  description: Modify internal/api/server.go — register s.mux.HandleFunc("/api/v2/working-hours", s.handleWorkingHours) in registerRoutes, wiring to the handler from T04.
  files_write: [internal/api/server.go]
  files_read: [internal/api/handler_working_hours.go]
  wave: 2
  depends_on: [T04]

- id: T08
  description: Modify web/src/explorer/Explorer.tsx — import WorkingHours, add open state, and add a menu/button entry to launch the modal, mirroring how MediaSettings/PrivacySettings are opened.
  files_write: [web/src/explorer/Explorer.tsx]
  files_read: [web/src/explorer/WorkingHours.tsx, web/src/explorer/PrivacySettings.tsx]
  wave: 2
  depends_on: [T05]
```

## DAG self-check

- **No write-write collision per wave:**
  - Wave 0: `internal/wa/working_hours.go`, `web/src/api.ts` — disjoint. ✓
  - Wave 1: `working_hours_test.go`, `handler_working_hours.go`, `WorkingHours.tsx`, `main.go` — disjoint. ✓
  - Wave 2: `server.go`, `Explorer.tsx` — disjoint. ✓
- **Wave monotonicity:** every depends_on points to a strictly lower wave. ✓
- **No placeholder paths:** all paths concrete. ✓
- **Wave 0 independent:** T01, T02 have empty depends_on. ✓

`wave_count = 3`, `max_wave_width = 4` (wave 1) → implementer fan-out 4 (at cap).

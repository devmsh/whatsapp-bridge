# WhatsApp Bridge

A self-hosted WhatsApp REST API bridge built on [whatsmeow](https://github.com/tulir/whatsmeow). Built to give AI agents full WhatsApp access — reading conversations, sending messages, scanning groups — through a clean HTTP API.

## Why I Built This

I run a startup studio ([One Studio](https://one-studio.co)) with multiple ventures across Saudi Arabia, Qatar, Turkey, and Palestine. WhatsApp is the primary communication layer — clients, teams, partners, investors all live there. I manage 40+ groups and hundreds of DMs across these ventures.

I'm building an agentic command center (HQ) where AI agents autonomously monitor these conversations, extract signals, update tasks, and even respond on my behalf. The agents needed a way to talk to WhatsApp programmatically.

### The journey

**V1 — Python MCP wrapper (Feb 2026):** Started with a Python MCP server wrapping whatsmeow's Go binary. It worked, but the architecture was fragile — Python calling Go through subprocess, MCP protocol adding latency, schema mismatches between what whatsmeow stored and what Python exposed. Every time whatsmeow updated, the Python layer broke.

**V2 — Pure Go, ground-up rewrite (Mar 2026):** Threw away the Python layer entirely. Built a clean Go service that wraps whatsmeow directly — one binary, one process, one database. Every whatsmeow event type gets its own handler. Every database table maps 1:1 to a whatsmeow concept. The REST API is the only interface — agents use `curl`, not MCP tools.

The key insight: **don't abstract whatsmeow, map it.** The V1 tried to create a "nice" API that hid whatsmeow's complexity. V2 exposes whatsmeow's full model (JIDs, message types, group metadata, presence, receipts) through a REST surface. If whatsmeow can do it, the bridge can do it.

### What the agents actually do with it

- **Intelligence scan**: Every few hours, agents pull all new messages via CSV endpoints, classify signals (progress, blockers, decisions, new contacts), and auto-update the task system
- **Commander's inbox**: My personal WhatsApp DM becomes a command interface — I send a voice note or text, the agent processes it, files it in HQ, and replies with confirmation
- **Group reports**: Weekly analytics reports posted directly to team WhatsApp groups
- **Delegation follow-ups**: Draft and send follow-up messages to team members about overdue tasks

## Features

- Full REST API (`/api/v2/`) — 30+ endpoints covering messages, contacts, groups, polls, newsletters, presence, privacy
- SQLite storage — 14 tables, WAL mode, all timestamps as Unix epoch
- CSV scan endpoints — bulk export of messages and groups for pipeline consumption
- Media handling — images, audio, video, documents, stickers with auto-conversion and thumbnail generation
- Full message operations — send, reply, react, edit, delete, forward, mention
- Group management — create, join, discover, update settings, manage participants
- Presence — typing indicators, online/offline status
- History sync — automatic backfill on first connection (3-6 months of messages)
- Periodic background sync — contacts and groups refreshed every 6 hours

## Quick Start

```bash
# Build the web UI + bridge into one binary
make build

# Run it
./whatsapp-bridge-v2

# Open the GUI (onboarding, QR, sync, explorer)
open http://localhost:8082/

# Health check
curl http://localhost:8082/api/v2/health
```

On first run the bridge is not linked. Open the GUI and scan the QR code with
WhatsApp (Settings → Linked Devices → Link a Device). The QR also prints in the
terminal for headless use. After linking, the phone pushes a full history sync,
and the GUI shows live progress.

## Web UI

A React + Vite + Tailwind app lives in `web/`. It is built to `web/dist` and
embedded into the Go binary with `go:embed`, so the production build is still a
single binary. The UI is served on the same port as the API and binds to
`127.0.0.1` only (set `BRIDGE_BIND=0.0.0.0` to expose it).

```bash
# One-off production build (UI + binary)
make build

# Frontend development with hot reload:
make run        # terminal 1 — the Go bridge on :8082
make dev-web    # terminal 2 — Vite on :5173, proxies /api to the bridge
```

What the UI covers today: onboarding (QR linking), live history-sync progress,
and a connected dashboard. A chat / media / contact explorer is next.

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `BRIDGE_PORT` | `8082` | HTTP API + web UI port |
| `BRIDGE_BIND` | `127.0.0.1` | Bind address (`0.0.0.0` to expose on the network) |
| `BRIDGE_DB_PATH` | `store/messages.db` | SQLite database path |
| `BRIDGE_WA_DB_PATH` | `store/whatsapp.db` | WhatsApp session database path |
| `BRIDGE_MEDIA_DIR` | `store` | Directory for downloaded media |
| `BRIDGE_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |

## API

All endpoints under `/api/v2/`:

### Messages
- `POST /api/v2/send` — Send a message (`{jid, message, media_path}`)
- `POST /api/v2/reply` — Reply to a message
- `POST /api/v2/react` — React with emoji
- `POST /api/v2/forward` — Forward to another chat
- `POST /api/v2/mention` — Send with @mentions
- `GET /api/v2/messages?chat=JID&limit=50` — Fetch messages
- `POST /api/v2/messages/{id}/edit` — Edit a sent message
- `POST /api/v2/messages/{id}/revoke` — Delete a message

### Contacts & Groups
- `GET /api/v2/contacts` — List all contacts
- `GET /api/v2/groups` — List all groups with full metadata
- `GET /api/v2/groups/discover` — Discover groups from contacts
- `POST /api/v2/groups/join` — Join via invite link

### Scan (Bulk CSV)
- `GET /api/v2/scan/messages?since=EPOCH` — All messages since timestamp as CSV
- `GET /api/v2/scan/groups?since=EPOCH` — New groups since timestamp as CSV

Both accept `&exclude=JID1,JID2` to filter out noise.

### Other
- `GET /api/v2/health` — Connection status, uptime, DB stats
- `POST /api/v2/polls` — Create a poll
- `POST /api/v2/presence/typing` — Send typing indicator
- `POST /api/v2/sync/contacts` — Trigger contact sync
- `GET /api/v2/newsletters` — List newsletter subscriptions
- `GET /api/v2/calls` — Call history

## Architecture

```
whatsapp-bridge/
  main.go                           Wiring: config -> DB -> WA client -> API server
  internal/
    config/config.go                Env-based configuration
    db/                             SQLite storage (14 tables)
      schema.go                     DDL, migrations
      messages.go                   Message CRUD
      contacts.go                   Contact CRUD + label management
      groups.go                     Group + participant CRUD
      ...                           calls, polls, reactions, receipts, etc.
    wa/                             WhatsApp client layer
      client.go                     whatsmeow wrapper + QR login
      dispatcher.go                 Event type -> handler routing
      handler_messages.go           Message events + media download
      handler_groups.go             Group join/leave/update events
      handler_contacts.go           Contact push name sync
      handler_history.go            History sync processing
      sync.go                       Periodic contact/group refresh
      media.go                      Download + image conversion + thumbnails
    api/                            REST API layer
      server.go                     Route registration (30+ endpoints)
      handler_send.go               Send, reply, react, forward, mention
      handler_scan.go               CSV bulk export for pipelines
      handler_groups.go             Group management
      handler_contacts.go           Contact search + details
      handler_*.go                  Other resource handlers
```

### Design Principles

1. **Map, don't abstract** — every whatsmeow type gets a 1:1 database table and API endpoint. No "simplified" models that lose data.
2. **Timestamps are integers** — Unix epoch everywhere. No timezone bugs, no format parsing.
3. **JIDs are the primary key** — `966535435254@s.whatsapp.net` for contacts, `120363406028992067@g.us` for groups. Stored as full strings.
4. **Media is local** — all images, audio, video, documents downloaded to `store/` on receipt. No external dependencies.
5. **One binary, one process** — no Python wrappers, no MCP servers, no subprocess chains.

## Data Storage

All data lives in `store/` (gitignored):
- `messages.db` — Messages, contacts, groups, polls, reactions, receipts, calls, presence
- `whatsapp.db` — WhatsApp session keys and encryption state (managed by whatsmeow — don't delete unless you want to re-scan QR)
- `images/`, `audio/`, `videos/`, `documents/` — Downloaded media files

## License

MIT — see [LICENSE](LICENSE).

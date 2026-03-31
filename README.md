# WhatsApp Bridge V2

A self-hosted WhatsApp bridge built on [whatsmeow](https://github.com/tulir/whatsmeow). Provides a REST API for reading and sending WhatsApp messages, managing contacts, groups, and media.

## Features

- Full REST API (`/api/v2/`) for messages, contacts, groups, polls, newsletters
- SQLite-backed message storage with full-text search
- Media handling (images, audio, video, documents, stickers) with auto-conversion
- CSV scan endpoints for bulk data export (messages + groups)
- Group management (create, join, discover, participants)
- Presence and typing indicators
- Poll creation and voting
- Reply, react, forward, mention, edit, delete
- Contact sync and history sync
- Periodic background sync

## Quick Start

```bash
# Build
go build -o whatsapp-bridge-v2

# Run (first time — scan the QR code)
./whatsapp-bridge-v2

# Health check
curl http://localhost:8082/api/v2/health
```

On first run, a QR code will be displayed in the terminal. Scan it with WhatsApp to link the device.

## Configuration

All configuration is via environment variables with sensible defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `BRIDGE_PORT` | `8082` | HTTP API port |
| `BRIDGE_DB_PATH` | `store/messages.db` | SQLite database path |
| `BRIDGE_WA_DB_PATH` | `store/whatsapp.db` | WhatsApp session database path |
| `BRIDGE_MEDIA_DIR` | `store` | Directory for downloaded media |
| `BRIDGE_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |

## API Overview

All endpoints are under `/api/v2/`. Key routes:

### Messages
- `GET /api/v2/messages?chat=JID&limit=50` — Fetch messages
- `POST /api/v2/send` — Send a text message
- `POST /api/v2/reply` — Reply to a message
- `POST /api/v2/react` — React to a message
- `POST /api/v2/forward` — Forward a message
- `POST /api/v2/mention` — Send with mentions

### Contacts & Groups
- `GET /api/v2/contacts` — List contacts
- `GET /api/v2/groups` — List groups
- `GET /api/v2/groups/discover` — Discover new groups
- `POST /api/v2/groups/join` — Join via invite link

### Scan (Bulk CSV Export)
- `GET /api/v2/scan/messages?since=EPOCH` — All messages since timestamp (CSV)
- `GET /api/v2/scan/groups?since=EPOCH` — Groups created since timestamp (CSV)

### Other
- `GET /api/v2/health` — Health check
- `GET /api/v2/chats` — List chats
- `POST /api/v2/polls` — Create a poll
- `GET /api/v2/newsletters` — List newsletters
- `POST /api/v2/sync/contacts` — Trigger contact sync

## Architecture

```
main.go                          Entry point
internal/
  config/config.go               Environment-based configuration
  db/                            SQLite storage layer (14 tables)
    schema.go                    DDL and migrations
    messages.go, contacts.go,    Per-entity CRUD
    groups.go, ...
  wa/                            WhatsApp client layer
    client.go                    whatsmeow wrapper
    dispatcher.go                Event routing
    handler_messages.go          Message processing + media download
    handler_groups.go            Group event handling
    handler_contacts.go          Contact sync
    sync.go                      Periodic sync
    media.go                     Media download + conversion
  api/                           HTTP REST API
    server.go                    Route registration
    handler_send.go              Send/reply/react/forward
    handler_scan.go              CSV bulk export
    handler_*.go                 Per-resource handlers
```

## Data Storage

All data is stored in `store/` (gitignored):
- `messages.db` — Messages, contacts, groups, media metadata
- `whatsapp.db` — WhatsApp session/encryption keys (managed by whatsmeow)
- `images/`, `audio/`, `videos/`, `documents/` — Downloaded media files

## License

MIT License. See [LICENSE](LICENSE).

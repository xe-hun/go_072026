# Notes Sync Backend

Backend service for an offline-first mobile notes app. It implements JWT-authenticated devices, ordered note blocks, diff-style sync operations, idempotent retries, soft deletion, append-only change history, snapshots, and a PostgreSQL outbox worker.

## Stack

- Go
- Chi
- PostgreSQL
- pgx/v5 + pgxpool
- sqlc query definitions
- golang-migrate migrations
- Supabase-compatible JWT/JWKS validation
- Gorilla WebSocket connections with PostgreSQL LISTEN/NOTIFY
- Docker Compose for local PostgreSQL
- `log/slog`

## Setup

1. Copy `.env.example` to `.env`.
2. Fill in `JWT_ISSUER`, `JWT_AUDIENCE`, and `JWT_JWKS_URL`.
3. Start PostgreSQL:

```sh
make db-up
```

4. Apply migrations:

```sh
make migrate-up
```

5. Run the API:

```sh
make run-api
```

6. Run the worker in a second terminal:

```sh
make run-worker
```

7. Run the WebSocket server in a third terminal:

```sh
make run-websocket
```

The API listens on port `8080` and the WebSocket server listens on
`WEBSOCKET_PORT` (`8082` by default).

## Development

Useful commands:

```sh
make migrate-version
make dev-auth
make dev-jwks
make sqlc
make test
go test -race ./...
make test-integration
make lint
make db-reset
```

If an existing empty database already has the initial schema from the previous
Goose migration, initialize golang-migrate bookkeeping with
`make migrate-force VERSION=1` instead of rerunning the initial migration.

`make db-reset` deletes the local PostgreSQL Docker volume after an explicit confirmation prompt.

For local or non-live testing without a real auth provider, generate a fake
one-year JWT and JWKS with `make dev-auth`, run the JWKS endpoint with
`make dev-jwks`, then copy the values from `.dev/auth/env` into `.env`. Use the
token in `.dev/auth/token.txt` as `Authorization: Bearer <token>`. The generated
files are ignored by git and must not be used for live user authentication.

VS Code debug configurations are included for the API, WebSocket server, and
worker in `.vscode/launch.json`.

## Endpoints

- `GET /health`
- `GET /ready`
- `POST /v1/devices`
- `GET /v1/devices`
- `DELETE /v1/devices/{deviceId}`
- `POST /v1/sync`
- `GET /v1/sync?protocolVersion=1&clientVersion=...&deviceId=...&cursor=...`
- `GET /v1/sync/updates-available?protocolVersion=1&deviceId=...`
- `GET /v1/realtime?deviceId=...` (WebSocket server, port `8082` by default)
- `GET /v1/notes/{noteId}`
- `GET /v1/notes/{noteId}/snapshot`

All `/v1` endpoints require `Authorization: Bearer <JWT>`.

## Sync Notes

Device registration requires a client-supplied `deviceId` string with 30
characters or fewer. The same value is used by the sync and revoke endpoints.

The sync POST endpoint accepts a batch of note/block operations without a cursor. Client operations
include a per-note integer `sequence`; the server sorts by `noteId` and then
`sequence`, applies each note as an atomic batch, and returns:

- accepted note batches with the note ID and one authoritative server version
- rejected note batches with the current server note snapshot

The sync GET endpoint pulls remote changes after its query-string cursor. It
returns `changes`, `hasMore`, and `nextCursor`; pulled changes expose
`globalSequence` and `resultingNoteVersion`, and omit `operationId` and `blockId`.
The updates-available endpoint uses the authenticated user's persisted device
cursor and returns `updatesAvailable` without fetching the change records.

`modify_note_property` replaces the note's properties with the supplied
`noteProperties` object, `modify_note_title` receives a nested `textDelta`,
`create_block` receives a nested `block`, and `modify_block` receives a
`blockProperties` object plus an optional nested `textDelta`. Each `textDelta` is
`{"index":0,"deleteCount":0,"insertText":"..."}`. Block IDs are
inside each operation's `changeData`. `create_category`, `modify_category`, and `delete_category` use
`{id,name}` or `{id}` payloads respectively; note categories are metadata,
not note columns. Current state updates and change-log inserts happen inside
the same PostgreSQL transaction. Operation idempotency is enforced by
`(device_id, client_operation_id)`.

## Realtime Notifications

An authenticated device may open `GET /v1/realtime?deviceId=<DEVICE_ID>`
using the same bearer token as the sync API. The server validates that the
device belongs to the authenticated user and is not revoked before upgrading
the request.

The WebSocket sends only small availability hints:

```json
{
  "type": "sync_available",
  "latestCursor": 8425
}
```

The client must compare `latestCursor` with its locally saved cursor and then
call the normal `/v1/sync` endpoint when newer data is available. A WebSocket
message must never advance the saved cursor. Notifications are coalesced to
the highest pending cursor, and the originating device is not notified.

Each WebSocket server instance keeps only its active sockets in memory.
PostgreSQL LISTEN/NOTIFY distributes committed availability events from the API
to the WebSocket process. Realtime delivery is best-effort and never changes
sync transaction results; clients catch up through normal cursor-based sync
after reconnects or missed notifications.

## Assumptions

- The client supplies a device ID string of at most 30 characters and normally
  generates note, block, and operation UUIDs.
- Text edits are stored as structured operations/changed fields, not raw character diffs.
- Automatic text conflict merging is intentionally not implemented in v1.
- Snapshot jobs are queued when configured thresholds are reached; cleanup/compaction hooks are present and deliberately conservative.

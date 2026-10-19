# Project Documentation

This backend is a modular monolith for an offline-first note-taking app. PostgreSQL is the single source of truth for current note state, device state, append-only change history, snapshots, and background jobs.

## High-Level Architecture

Request flow:

```text
HTTP request
  -> middleware
  -> handler
  -> service
  -> store transaction
  -> sqlc-generated PostgreSQL query
  -> service response mapping
  -> JSON HTTP response
```

The API process is stateless. Durable state lives in PostgreSQL. Multiple API instances can run at the same time because sync writes lock note/device rows in PostgreSQL transactions.

## Runtime Units

`cmd/api` starts the HTTP server. It loads configuration, opens `pgxpool`, initializes JWT verification, builds routes, and performs graceful shutdown.

`cmd/websocket` starts the dedicated WebSocket server. It loads configuration,
opens its own `pgxpool` for connection-time device validation, subscribes to
PostgreSQL `LISTEN/NOTIFY`, and owns the in-memory connection hub.

`cmd/worker` starts the background worker. It loads configuration, opens `pgxpool`, claims jobs from `outbox_jobs`, and currently creates note snapshots.

`internal/config` loads `.env` and environment variables. It validates required settings at startup so configuration errors fail fast.

`internal/auth` validates JWT bearer tokens against a JWKS endpoint. It extracts the token subject as the authenticated user UUID and stores it in request context.

`internal/middleware` contains shared HTTP middleware:

- request IDs
- structured request logging
- panic recovery
- request body limits
- gzip request decompression
- gzip response compression
- timeout handling
- rate limiter interface

`internal/httpapi` contains API response helpers, request validation, request ID context helpers, and the consistent JSON error envelope.

`internal/devices` handles device registration, listing, and revocation. Its
models own request/entity conversions and device validation.

`internal/notes` handles direct current-state reads and latest snapshot reads.
Note and block models own entity-to-response conversion and entity validation.

`internal/sync` contains the main offline sync protocol, operation model
conversion/validation, idempotency, version conflict handling, current-state
mutations, change-log writes, pull pagination, and snapshot job enqueueing.

`internal/realtime` owns active local WebSocket connections and the
PostgreSQL LISTEN/NOTIFY event bus. The WebSocket process stores only
authenticated user/device-to-connection mappings and never performs note
synchronization.

`internal/store` is the persistence boundary. It wraps `pgxpool`, coordinates transactions, converts generated sqlc models into app-facing models, and exposes store methods used by services.

`internal/jobs` contains the outbox worker and snapshot creation logic.

`db/migrations` contains golang-migrate migrations.

`db/queries` contains sqlc query definitions.

`db/generated` contains sqlc-generated code. These files should not be manually edited because `make sqlc` rewrites them.

`tests/integration` contains integration tests gated behind the `integration` build tag.

## External Dependencies

Application dependencies:

- `github.com/go-chi/chi/v5`: HTTP routing
- `github.com/golang-jwt/jwt/v5`: JWT parsing and claims validation
- `github.com/google/uuid`: UUID generation and parsing
- `github.com/jackc/pgx/v5`: PostgreSQL driver and connection pool
- `github.com/gorilla/websocket`: WebSocket upgrade and frame handling

Tooling dependencies:

- `github.com/sqlc-dev/sqlc/cmd/sqlc`: SQL-to-Go code generation
- `github.com/golang-migrate/migrate/v4/cmd/migrate`: migrations
- Docker Compose: local PostgreSQL

## Database Model

`categories` stores user-owned note categories.

`notes` stores note-level state: owner, title, note-properties JSON, current note version, timestamps, and tombstone deletion timestamp. Category data is supplied through note properties.

`note_blocks` stores ordered blocks and block-properties JSON. Blocks use string positions for fractional indexing so inserts and moves usually update only one row.

`sync_devices` stores per-user devices keyed by a client-supplied string device
ID of at most 30 characters, along with protocol version, last seen timestamp,
revocation timestamp, and last global cursor.

`note_changes` is append-only. Every accepted operation creates exactly one change record with base/resulting versions and a global sequence.

`note_snapshots` stores periodic full-note snapshots with checksums and the
`last_applied_cursor` global change sequence represented by each snapshot.

`outbox_jobs` stores background jobs. The worker safely claims jobs using row locking and `SKIP LOCKED`.

## API Flow

The API server starts in `cmd/api/main.go`:

1. Load and validate configuration.
2. Open a PostgreSQL pool.
3. Initialize JWKS-backed JWT verification.
4. Create the PostgreSQL event bus used to publish committed sync hints.
5. Build Chi routes and attach middleware.
6. Serve until interrupted.
7. Stop HTTP intake and close database connections.

Public routes:

- `GET /health`
- `GET /ready`

Authenticated routes:

- `POST /v1/devices`
- `GET /v1/devices`
- `DELETE /v1/devices/{deviceId}`
- `POST /v1/sync`
- `GET /v1/sync/updates-available?protocolVersion=1&deviceId=...`
- `GET /v1/notes/{noteId}`
- `GET /v1/notes/{noteId}/snapshot`

The WebSocket server exposes:

- `GET /health`
- `GET /ready`
- `GET /v1/realtime?deviceId=...`

## Authentication Flow

The auth middleware:

1. Reads `Authorization: Bearer <JWT>`.
2. Parses the JWT.
3. Selects the RSA public key from JWKS using `kid`.
4. Validates signature, issuer, audience, and expiry.
5. Parses the subject as a UUID.
6. Adds `auth.Claims` to request context.

Services never trust client-supplied owner IDs. They use the authenticated user ID from context.

## Device Flow

Device registration:

1. Handler decodes JSON.
2. Service validates the client-supplied device ID and defaults protocol version to `1`.
3. Service rejects unsupported protocol versions.
4. Store inserts a `sync_devices` row scoped to the authenticated user.
5. Handler returns the device record.

Device revocation soft-disables a device by setting `revoked_at`. Sync rejects revoked devices with `DEVICE_REVOKED`.

## Sync Flow

The sync endpoint receives a batch:

```text
protocol version
client version
device id
cursor
pull limit
operations[]
```

The service:

1. Validates request-level fields and limits.
2. Starts a PostgreSQL transaction.
3. Locks the device row and verifies ownership/revocation.
4. Sorts operations by `noteId` and then client `sequence`.
5. Applies each note's operations as one savepoint-backed batch.
6. Looks up `(device_id, operation_id)` to make retries idempotent.
7. Validates operation type, change format, direct changeData shapes, and block IDs contained in those shapes.
8. Locks and loads the complete note document once for each note batch.
9. Compares client note base versions to the loaded server version.
10. Applies valid operations in sequence to the in-memory document.
11. Inserts exactly one `note_changes` row per accepted operation.
12. Rolls back the whole note batch if any operation fails and returns one rejected DTO with the current note snapshot, including blocks.
13. Saves the complete changed note document and increments its version once.
14. Enqueues snapshot jobs when thresholds are reached.
15. Pulls remote changes after the client cursor.
16. Updates the device cursor and last seen timestamp.
17. Commits the transaction.
18. Returns accepted and rejected DTOs, pulled changes, next cursor, and pagination state.

Clients can call `GET /v1/sync/updates-available` with a protocol version and
device ID to check the authenticated user's persisted device cursor without
pulling change records. The response is `{ "updatesAvailable": true|false }`.

A valid sync request can have both accepted and rejected note batches. These are returned with HTTP `200 OK`. Whole-request errors, such as invalid protocol or revoked device, use normal error responses.

## Realtime Flow

The authenticated `GET /v1/realtime?deviceId=...` handler validates the
registered device with the shared store, upgrades the request, and registers
the connection in the local hub. Each socket has a small bounded send queue,
read/write deadlines, a ping/pong heartbeat, and a small incoming frame limit.
Slow clients are disconnected; pending `sync_available` hints are coalesced to
the highest cursor.

The WebSocket process starts its PostgreSQL listener on a dedicated connection
and forwards events to its local hub. After a sync transaction commits, the sync service publishes one event for the
highest newly inserted `note_changes.global_sequence`:

```json
{
  "userId": "uuid",
  "originDeviceId": "phone",
  "latestCursor": 8425
}
```

The PostgreSQL event bus publishes with `pg_notify('sync_available', ...)`.
Every WebSocket server instance listens on a dedicated connection acquired from
its `pgxpool`, validates incoming payloads, and forwards events only to local
connections for the event's user. The originating device is skipped.

The client-facing message is:

```json
{
  "type": "sync_available",
  "latestCursor": 8425
}
```

This is only a hint. Clients must not advance their saved cursor from it;
only a successful normal sync response may advance the cursor. Publishing is
best-effort after commit, so a PostgreSQL notification failure cannot roll
back or turn a completed sync write into an error.

During WebSocket server shutdown, HTTP shutdown stops new upgrades, the
listener context is canceled and its acquired connection is released, then the
hub closes active sockets and waits for their read/write loops before the store
is closed.

## Versioning

Each successful note operation batch increments `notes.current_version` once; all
accepted operations in that batch share the resulting version.

The server is authoritative for resulting versions.

Version mismatches return `BASE_VERSION_CONFLICT`; the code does not silently overwrite note or block text.

## Idempotency

Each operation has a stable `operationId`.

The database enforces uniqueness on `(device_id, client_operation_id)`.

Before applying an operation, sync checks whether the operation was already processed. If so, it maps the previous `note_changes` row back into an accepted response and does not mutate state again.

## Snapshot Worker Flow

The worker:

1. Claims one available `outbox_jobs` row using `FOR UPDATE SKIP LOCKED`.
2. Dispatches by `job_type`.
3. For `create_snapshot`, loads note and blocks in a transaction.
4. Reads the highest `note_changes.global_sequence` for the note and carries it
   as `lastAppliedCursor`.
5. Builds a full snapshot document.
6. Marshals the snapshot to JSON.
7. Computes a SHA-256 checksum of the JSON.
8. Inserts or updates the snapshot row for that note/version.
9. Marks the job complete.
10. On failure, stores the error and schedules a retry.

Conflict and full-note sync snapshots expose the same `lastAppliedCursor` field
so a client can resume change replay from the cursor represented by the full
state.

## Error Format

All normal API errors use:

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "The request is invalid.",
    "requestId": "uuid",
    "details": {}
  }
}
```

Raw database errors, JWT internals, stack traces, note text, and full request bodies are not returned to clients.

## Generated Code Policy

`db/generated` is generated by sqlc and should not be edited manually. Add or change SQL in `db/queries`, then run:

```sh
make sqlc
```

The hand-written `internal/store` package is the stable boundary used by services.

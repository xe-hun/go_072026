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

`internal/storage` is the trusted Supabase Storage HTTP client. It creates
signed upload/download capabilities, reads object metadata, and deletes stored
objects without exposing the service credential.

`internal/uploads` owns upload authorization, note/block ownership checks,
metadata validation, and lifecycle transitions. File bytes never pass through
the Go API.

### Supabase Storage configuration

Set `SUPABASE_URL`, `SUPABASE_SECRET_KEY`, and `SUPABASE_STORAGE_BUCKET` in the
API and worker environments. The bucket must be private. Configure its file
size limit and allowed MIME types to match `SUPABASE_STORAGE_MAX_FILE_SIZE`
and `SUPABASE_STORAGE_ALLOWED_MIME_TYPES`; the API enforces the same limits
before requesting an authorization. `SUPABASE_STORAGE_DOWNLOAD_TTL_SECONDS`
controls short-lived download links, while upload authorizations use the
documented two-hour Supabase lifetime by default. The service key is trusted
server configuration and must never be included in frontend configuration.
For browser clients hosted on another origin, configure Storage/deployment
CORS to allow that origin's direct `PUT` requests and the required content
headers.

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

`file_uploads` stores the application record for each note/block attachment.
Each row is linked to the authenticated owner, note, and block, and stores the
private bucket/path rather than a temporary signed URL or token. Each row also
has an append-only upload cursor and the registered device that initiated it.
The lifecycle is `PENDING`, `COMPLETED`, `FAILED`, `EXPIRED`, `CANCELLED`, or
`DELETED`.

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
- `POST /v1/uploads/initiate`
- `POST /v1/uploads/{uploadId}/complete`
- `GET /v1/uploads/{uploadId}`
- `POST /v1/uploads/{uploadId}/download-url`
- `GET /v1/uploads/download-links?cursor=...&deviceId=...`
- `DELETE /v1/uploads/{uploadId}`

### Upload lifecycle

`POST /v1/uploads/initiate` accepts `noteId`, `blockId`, registered `deviceId`,
`filename`, `contentType`, and positive `size`. The service authenticates the
owner, checks that the device belongs to that owner and is not revoked, checks
that the block belongs to the note, validates the configured MIME and size
limits, writes a pending row, and requests a Supabase signed upload URL.
The response contains `uploadId`, `cursor`, `bucket`, `path`, `signedUrl`,
`token`, and `expiresAt`. The token is transient and is not stored or logged.

The browser uploads directly to the private bucket using the signed URL. It
then calls `POST /v1/uploads/{uploadId}/complete` without resubmitting the
path. The API retrieves the stored path from PostgreSQL, asks Supabase for
object info, compares the actual size and MIME type to the declared metadata,
and uses a conditional update so concurrent completion requests are safe.
Already completed uploads return their existing result.

For a plain browser client, the direct transfer is equivalent to:

```js
await fetch(initiation.signedUrl, {
  method: "PUT",
  headers: { "content-type": initiation.contentType },
  body: file,
});
await fetch(`/v1/uploads/${initiation.uploadId}/complete`, {
  method: "POST",
  headers: { Authorization: `Bearer ${accessToken}` },
});
```

The browser should use the declared content type, avoid storing the signed
URL longer than needed, and retry completion/status rather than starting a
second upload after a lost response.

`GET /v1/uploads/{uploadId}` exposes lifecycle metadata to the authenticated
owner.
`POST /v1/uploads/{uploadId}/download-url` issues a short-lived private signed
download URL only for completed, authorized files. `DELETE` removes the
trusted stored object path and then marks the row `DELETED`; missing objects
are treated as an idempotent deletion.

`GET /v1/uploads/download-links?cursor=<cursor>&deviceId=<deviceId>` returns
signed download URLs for completed uploads owned by the authenticated user
whose upload cursor is after the supplied cursor, excluding uploads initiated
by the supplied device. It advances through terminal rows and the requesting
device's rows without returning links for them. Pending or problematic uploads
are skipped so ready files can still be returned; clients that need to retry a
skipped upload can use its individual status/download endpoints. The response
shape is `{ "links": [...], "nextCursor": 123 }`; clients should persist
`nextCursor` after processing the links.

The worker reconciles pending rows after `expiresAt` plus
`SUPABASE_STORAGE_CLEANUP_GRACE_SECONDS`. It verifies any late object before
finalizing it, deletes mismatched objects, and marks missing or invalid rows
expired. The grace period does not revoke a previously issued Supabase token;
it only reduces cleanup races.

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

## Note Synchronisation Guide

Note synchronisation is a push/pull protocol keyed by the registered device
identifier and a per-user global change cursor. The cursor belongs to the note
change feed, not to file-upload cursors.

The normal client flow is:

1. Register one device with `POST /v1/devices` and persist its `deviceId`.
2. Apply edits locally and queue operations with stable `operationId` values,
   the note's `baseNoteVersion`, and a per-note `sequence`.
3. Send queued operations with `POST /v1/sync`. The server authenticates the
   user, locks the device and affected notes, orders operations, and applies
   each note batch transactionally. The current note state and its append-only
   `note_changes` rows commit together.
4. Treat accepted batches as server-authoritative versions. For a rejected
   batch, use the returned note snapshot and its `lastAppliedCursor` to rebuild
   local state before retrying.
5. Pull remote changes with `GET /v1/sync` using the saved cursor and the same
   `deviceId`. The server excludes changes written by that device, returns
   changes in global-sequence order, and returns `nextCursor` for the page.
6. Apply the page, then persist `nextCursor`. When `hasMore` is true, request
   another page from that cursor; never advance the cursor from a WebSocket
   hint alone.
7. Use `GET /v1/sync/updates-available` or the realtime `sync_available`
   message as a wake-up signal. A hint only says that a newer change may exist;
   the normal pull endpoint remains the source of truth.

Retries are safe because operations are idempotent by `(device_id,
operation_id)`. A version conflict rejects the complete note batch rather than
partially applying it. Deleted notes and blocks remain as tombstones so their
deletions can reach offline devices. Full-note responses and snapshots include
the cursor represented by that state, allowing a client to resume pull sync
after replacing local state.

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

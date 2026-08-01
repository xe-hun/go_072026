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

`internal/devices` handles device registration, listing, and revocation.

`internal/notes` handles direct current-state reads and latest snapshot reads.

`internal/sync` contains the main offline sync protocol, operation validation, idempotency, version conflict handling, current-state mutations, change-log writes, pull pagination, and snapshot job enqueueing.

`internal/store` is the persistence boundary. It wraps `pgxpool`, coordinates transactions, converts generated sqlc models into app-facing models, and exposes store methods used by services.

`internal/jobs` contains the outbox worker and snapshot creation logic.

`db/migrations` contains Goose migrations.

`db/queries` contains sqlc query definitions.

`db/generated` contains sqlc-generated code. These files should not be manually edited because `make sqlc` rewrites them.

`tests/integration` contains integration tests gated behind the `integration` build tag.

## External Dependencies

Application dependencies:

- `github.com/go-chi/chi/v5`: HTTP routing
- `github.com/golang-jwt/jwt/v5`: JWT parsing and claims validation
- `github.com/google/uuid`: UUID generation and parsing
- `github.com/jackc/pgx/v5`: PostgreSQL driver and connection pool

Tooling dependencies:

- `github.com/sqlc-dev/sqlc/cmd/sqlc`: SQL-to-Go code generation
- `github.com/pressly/goose/v3/cmd/goose`: migrations
- Docker Compose: local PostgreSQL

## Database Model

`categories` stores user-owned note categories.

`notes` stores note-level state: owner, category, title, metadata JSON, current note version, timestamps, and tombstone deletion timestamp.

`note_blocks` stores ordered blocks. Blocks use string positions for fractional indexing so inserts and moves usually update only one row.

`sync_devices` stores per-user devices, protocol version, last seen timestamp, revocation timestamp, and last global cursor.

`note_changes` is append-only. Every accepted operation creates exactly one change record with base/resulting versions and a global sequence.

`note_snapshots` stores periodic full-note snapshots with checksums.

`outbox_jobs` stores background jobs. The worker safely claims jobs using row locking and `SKIP LOCKED`.

## API Flow

The API server starts in `cmd/api/main.go`:

1. Load and validate configuration.
2. Open a PostgreSQL pool.
3. Initialize JWKS-backed JWT verification.
4. Build Chi routes.
5. Attach middleware.
6. Serve until interrupted.
7. Shut down gracefully and close database connections.

Public routes:

- `GET /health`
- `GET /ready`

Authenticated routes:

- `POST /v1/devices`
- `GET /v1/devices`
- `DELETE /v1/devices/{deviceId}`
- `POST /v1/sync`
- `GET /v1/notes/{noteId}`
- `GET /v1/notes/{noteId}/snapshot`

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
2. Service defaults protocol version to `1`.
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
4. Processes each operation independently.
5. Looks up `(device_id, operation_id)` to make retries idempotent.
6. Validates entity type, operation type, block ID requirements, change format, and change schema.
7. Locks affected note and block rows with `FOR UPDATE`.
8. Compares client base versions to server versions.
9. Applies valid operations to current state.
10. Inserts exactly one `note_changes` row per accepted operation.
11. Enqueues snapshot jobs when thresholds are reached.
12. Pulls remote changes after the client cursor.
13. Updates the device cursor and last seen timestamp.
14. Commits the transaction.
15. Returns accepted operations, rejected operation items, pulled changes, next cursor, and pagination state.

A valid batch can have both accepted and rejected operations. These are returned with HTTP `200 OK`. Whole-request errors, such as invalid protocol or revoked device, use normal error responses.

## Versioning

Every mutation increments `notes.current_version`.

Every block mutation also increments `note_blocks.current_version`.

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
4. Builds a full snapshot document.
5. Marshals the snapshot to JSON.
6. Computes a SHA-256 checksum of the JSON.
7. Inserts or updates the snapshot row for that note/version.
8. Marks the job complete.
9. On failure, stores the error and schedules a retry.

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


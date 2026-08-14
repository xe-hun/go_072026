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

## Development

Useful commands:

```sh
make migrate-version
make dev-auth
make dev-jwks
make sqlc
make test
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

VS Code debug configurations are included for the API and worker in `.vscode/launch.json`.

## Endpoints

- `GET /health`
- `GET /ready`
- `POST /v1/devices`
- `GET /v1/devices`
- `DELETE /v1/devices/{deviceId}`
- `POST /v1/sync`
- `GET /v1/notes/{noteId}`
- `GET /v1/notes/{noteId}/snapshot`

All `/v1` endpoints require `Authorization: Bearer <JWT>`.

## Sync Notes

The sync endpoint accepts a batch of note/block operations. Client operations
include a per-note integer `sequence`; the server sorts by `noteId` and then
`sequence`, applies each note as an atomic batch, and returns:

- accepted note batches with authoritative note versions and global sequence
- rejected note batches with the current server note snapshot
- pulled changes after the supplied cursor
- the next cursor

`update_block` text changes use `textDelta`, `textOperationType` (`insert` or
`delete`), and `index`. Current state updates and change-log inserts happen
inside the same PostgreSQL transaction. Operation idempotency is enforced by
`(device_id, client_operation_id)`.

## Assumptions

- The client normally generates note, block, device, and operation UUIDs.
- Text edits are stored as structured operations/changed fields, not raw character diffs.
- Automatic text conflict merging is intentionally not implemented in v1.
- Snapshot jobs are queued when configured thresholds are reached; cleanup/compaction hooks are present and deliberately conservative.

# Run Instructions

This file explains the next steps to move the project from source code to a locally working Notes Sync backend.

## 1. Prerequisites

Install or verify these tools:

- Go 1.26 or newer
- Docker Desktop with Docker Compose
- PowerShell or a terminal that can run `make`
- Optional: VS Code with the Go extension

Check the local tools:

```sh
go version
docker version
docker compose version
```

## 2. Create the Local Environment File

Copy the example environment file:

```sh
copy .env.example .env
```

On PowerShell, this also works:

```powershell
Copy-Item .env.example .env
```

Open `.env` and fill in the JWT settings:

```env
JWT_ISSUER=
JWT_AUDIENCE=
JWT_JWKS_URL=
```

For Supabase Auth these values normally come from the project auth settings. The API will fail fast on startup if any required value is missing.

## 3. Start PostgreSQL

Start the local database container:

```sh
make db-up
```

Confirm that the database is healthy:

```sh
docker ps
```

The container should be named `notes-postgres`.

## 4. Apply Database Migrations

Run the Goose migrations:

```sh
make migrate-up
```

This creates:

- `categories`
- `notes`
- `note_blocks`
- `sync_devices`
- `note_changes`
- `note_snapshots`
- `outbox_jobs`

## 5. Generate SQL Code

The generated sqlc package is already present, but regenerate it after changing files in `db/queries` or `db/migrations`:

```sh
make sqlc
```

## 6. Run the API

Start the HTTP API:

```sh
make run-api
```

The default address is:

```text
http://localhost:8080
```

Health check:

```sh
curl http://localhost:8080/health
```

Readiness check:

```sh
curl http://localhost:8080/ready
```

`/health` confirms the process is alive. `/ready` confirms PostgreSQL is reachable.

## 7. Run the Worker

Open a second terminal and start the background worker:

```sh
make run-worker
```

The worker claims `outbox_jobs` rows and currently handles snapshot creation jobs.

## 8. Register a Device

All `/v1` routes require a JWT:

```text
Authorization: Bearer <JWT>
```

Register a device:

```sh
curl -X POST http://localhost:8080/v1/devices \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d "{\"deviceName\":\"Local Dev\",\"platform\":\"windows\",\"appVersion\":\"1.0.0\",\"protocolVersion\":1}"
```

Save the returned device `id`. It is required by `/v1/sync`.

## 9. Send an Empty Sync Request

```sh
curl -X POST http://localhost:8080/v1/sync \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d "{\"protocolVersion\":1,\"clientVersion\":\"1.0.0\",\"deviceId\":\"<DEVICE_ID>\",\"cursor\":0,\"limit\":500,\"operations\":[]}"
```

Expected response shape:

```json
{
  "accepted": [],
  "rejected": [],
  "changes": [],
  "nextCursor": 0,
  "hasMore": false,
  "resyncRequired": false
}
```

## 10. Create a Note Through Sync

Generate UUIDs for the operation and note. Then send:

```json
{
  "protocolVersion": 1,
  "clientVersion": "1.0.0",
  "deviceId": "<DEVICE_ID>",
  "cursor": 0,
  "limit": 500,
  "operations": [
    {
      "operationId": "<OPERATION_ID>",
      "noteId": "<NOTE_ID>",
      "entityType": "note",
      "operationType": "create_note",
      "baseNoteVersion": 0,
      "changeFormat": "structured-operation-v1",
      "changeData": {
        "schemaVersion": 1,
        "fields": {
          "title": "Shopping",
          "metadata": {
            "isPinned": true
          }
        }
      }
    }
  ]
}
```

The accepted response should return note version `1` and a global sequence.

## 11. Create a Block Through Sync

Use the note version returned from the note creation response:

```json
{
  "protocolVersion": 1,
  "clientVersion": "1.0.0",
  "deviceId": "<DEVICE_ID>",
  "cursor": 0,
  "limit": 500,
  "operations": [
    {
      "operationId": "<OPERATION_ID>",
      "noteId": "<NOTE_ID>",
      "blockId": "<BLOCK_ID>",
      "entityType": "block",
      "operationType": "create_block",
      "baseNoteVersion": 1,
      "changeFormat": "structured-operation-v1",
      "changeData": {
        "schemaVersion": 1,
        "fields": {
          "blockType": "todo",
          "textContent": "Buy milk",
          "position": "a0",
          "properties": {
            "isChecked": false
          }
        }
      }
    }
  ]
}
```

## 12. Read Current Note State

```sh
curl http://localhost:8080/v1/notes/<NOTE_ID> \
  -H "Authorization: Bearer <JWT>"
```

This returns the note row and ordered blocks.

## 13. Run Tests and Checks

Run unit tests:

```sh
make test
```

Run vet:

```sh
make lint
```

Build API and worker:

```sh
go build ./cmd/api ./cmd/worker
```

Run integration tests when a test database is available:

```sh
set TEST_DATABASE_URL=postgres://notes_user:notes_password@localhost:5432/notes_db?sslmode=disable
make test-integration
```

PowerShell:

```powershell
$env:TEST_DATABASE_URL="postgres://notes_user:notes_password@localhost:5432/notes_db?sslmode=disable"
make test-integration
```

## 14. Reset Local Data

Only do this when local data can be deleted:

```sh
make db-reset
```

The command asks for explicit confirmation before deleting the Docker volume.

## 15. Debugging in VS Code

Use the included `.vscode/launch.json` configurations:

- `Run Notes API`
- `Run Notes Worker`

Both load `.env` and use the workspace root as the current working directory.

## 16. Common Startup Problems

`missing required environment variables`: Fill in `JWT_ISSUER`, `JWT_AUDIENCE`, and `JWT_JWKS_URL`.

`database is not ready`: Start Docker, run `make db-up`, and check that port `5432` is not already used.

`invalid token`: Confirm the JWT issuer, audience, signature, expiry, and JWKS URL match the auth provider.

`relation does not exist`: Run `make migrate-up`.

`device does not belong to authenticated user`: Register the device using the same JWT user before syncing.


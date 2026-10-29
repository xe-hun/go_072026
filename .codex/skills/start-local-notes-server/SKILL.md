---
name: start-local-notes-server
description: Start the Windows local Notes Sync backend environment when the user asks to run this repository locally, including Docker PostgreSQL, development JWKS, WebSocket, and API processes.
---

# Start Local Notes Server

Use this skill only for the Notes Sync Backend repository described by its
`AGENTS.md`. It starts the local development environment; it does not start
the worker, pgAdmin, or reset/delete database data.

Run the bundled PowerShell orchestrator from the repository root:

```powershell
& (Join-Path $skillRoot 'scripts/start-local-notes-server.ps1') -RepoRoot (Get-Location).Path
```

Resolve `$skillRoot` to the directory containing this `SKILL.md`. If the
current directory is not the repository root, locate the root by finding
`AGENTS.md`, `go.mod`, `compose.yaml`, and `run-instruction.md` together, then
pass that directory as `-RepoRoot`.

The orchestrator follows `run-instruction.md` and performs these checks and
actions in order:

1. Verify Go, Docker, Docker Compose, the repository files, and required
   configuration. If `.env` is absent, create it from `.env.example`.
2. Generate the ignored development JWT/JWKS fixture in `.dev/auth` when it is
   missing. Keep the configured issuer and audience when present, and make
   `JWT_JWKS_URL` point to the local JWKS server at port 8081.
3. Confirm Docker Desktop is available and running, starting it when the
   executable can be found. Start only the `postgres` Compose service and wait
   for its health check.
4. Apply pending migrations using the repository's golang-migrate command.
5. Start the development JWKS server, WebSocket server, and API in separate
   visible PowerShell windows. Wait for each health/readiness endpoint before
   reporting success. Existing healthy instances are reused.

The script stops before launching processes when required values are missing.
Never invent `SUPABASE_URL` or `SUPABASE_SECRET_KEY`; ask the user to fill
those values in `.env` and invoke the skill again. The script may update only
the local `.env` JWT settings needed to match the development JWKS fixture.

For a non-mutating preflight, use:

```powershell
& (Join-Path $skillRoot 'scripts/start-local-notes-server.ps1') -RepoRoot (Get-Location).Path -ValidateOnly
```

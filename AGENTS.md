# Coding Agent Instructions

These instructions apply to the whole repository unless a more-specific
`AGENTS.md` or `AGENTS.override.md` applies to the target path. They are for
Codex, Copilot, reviewers, and delegated agents working on the Notes Sync
Backend.

## Project context

This repository is a Go backend for an offline-first notes application. It is
a modular monolith backed by PostgreSQL and currently provides:

- JWT/JWKS-authenticated device and note APIs through Chi.
- Transactional note/block/category sync with conflict handling and
  idempotent operation retries.
- Append-only change history, cursors, snapshots, and a PostgreSQL outbox.
- A separate WebSocket process that delivers best-effort sync-availability
  hints through PostgreSQL `LISTEN/NOTIFY`.
- A separate worker process for outbox jobs, currently snapshot creation.

The module is `notes-server` and targets Go 1.26.5. Read the current code and
database migrations as the source of truth; README examples and design notes
are useful context but must not override implemented behavior.

## Session directives

Treat these plain-text tokens as session-scoped directives. Recognize both
bare forms such as `/dev` and wrapped forms such as `-/dev-`. A newer,
more-specific user instruction or a higher-priority system instruction wins.

### `/dev`

Use development-stage implementation rules:

- Implement the requested behavior directly in the current code.
- Do not add backward compatibility, legacy support, migration shims,
  duplicate code paths, deprecation layers, or fallback behavior solely to
  preserve old callers.
- Breaking local API, schema, configuration, route, generated-code, or test
  changes are acceptable when they are a direct consequence of the task.
- Preserve correctness, security, data integrity, secrets hygiene, and
  unrelated user work.

### `/no-test`

Do not create, update, regenerate, or run tests for the active task. Non-test
checks such as formatting, code generation, builds, or static analysis may
still run when useful. State clearly in the handoff that tests were skipped
because this directive was active.

### `/docu`

Update the appropriate existing documentation for code changes made during
the active task. Use `README.md`, `documentation.md`, `run-instruction.md`, or
another project document that already fits. Document user-visible behavior,
setup or command changes, API/schema/configuration changes, and intentional
breaking changes concisely.

Do not copy these directive tokens into source code, generated files, commit
messages, or ordinary project documentation unless the task is specifically
about the directives.

## Before editing

- Read this instruction chain from the repository root to the target file.
- Inspect `git status` and the relevant diff before changing anything. Preserve
  unrelated user changes, including staged, unstaged, and untracked work.
- Read only the project documents relevant to the task:
  - `README.md` for setup, endpoints, and high-level behavior.
  - `documentation.md` for architecture, data flow, and invariants.
  - `run-instruction.md` for local operation and troubleshooting.
  - `codex_notes_sync_spec.md` for sync or protocol design context; verify it
    against current code because the code and migrations take precedence.
  - `Makefile`, `compose.yaml`, `sqlc.yaml`, and `.env.example` for commands,
    services, generation, and configuration.
- If a needed repository skill or referenced document is absent, do not invent
  its rules. Continue with the current source and report the missing reference.
- Keep the change limited to the requested behavior. Avoid speculative
  abstractions, unrelated refactors, new dependencies, broad formatting, and
  compatibility code that was not requested.

## Architecture and ownership

Keep the existing dependency direction and boundaries:

- `cmd/api`, `cmd/websocket`, `cmd/worker`, and `cmd/devauth` are process
  entrypoints and composition roots. They load configuration, wire concrete
  dependencies, and own process lifecycle; business rules belong in `internal`.
- `internal/*/handlers` decode requests, invoke services, and write responses.
  Keep HTTP concerns out of domain and persistence code.
- `internal/devices`, `internal/notes`, and `internal/sync` own their
  application rules and models. Services must use the authenticated user from
  request context rather than trusting a client-supplied owner ID.
- `internal/store` is the PostgreSQL and sqlc persistence boundary. Keep
  transactions, row locking, generated-query use, and database model
  conversion there unless the established pattern requires otherwise.
- `internal/auth`, `internal/httpapi`, and `internal/middleware` provide the
  shared authentication, response/error, validation, and HTTP middleware
  boundaries.
- `internal/realtime` owns the in-memory WebSocket hub and PostgreSQL event
  bus. It sends availability hints only; it does not perform note sync.
- `internal/jobs` owns outbox claiming and snapshot processing.
- `db/migrations` is the migration source of truth, `db/queries` is the sqlc
  query source, and `db/generated` is derived output.
- `tests/integration` contains tests using the `integration` build tag and a
  reachable `TEST_DATABASE_URL`.

Use constructor injection and explicit dependencies. Do not introduce global
database, logger, configuration, or service state. Propagate `context.Context`
through I/O and database operations. Use `log/slog` for structured logging and
avoid exposing JWT details, credentials, raw database errors, note contents,
or full request bodies in client responses or logs.

## Database, sync, and realtime invariants

When changing persistence or synchronization behavior, preserve the invariants
documented by the current implementation:

- Current note state and its append-only change records are written in the
  same PostgreSQL transaction.
- Device ownership and revocation are checked against authenticated users.
- Operation idempotency is keyed by the existing device/operation identity;
  retries must not apply state mutations twice.
- Note operation batches preserve their current ordering, version checks,
  conflict behavior, and rejected-batch semantics.
- Cursor updates occur only through the normal sync flow. A WebSocket message
  is a hint and must not advance a client cursor.
- Realtime publication is best-effort after commit and must not turn a
  completed sync transaction into a failed write.
- Outbox jobs are claimed and completed using the existing transaction and
  retry patterns; snapshot creation must remain idempotent.

For changes involving sync, conflicts, cursors, migrations, snapshots, jobs,
or WebSockets, read the relevant sections of `documentation.md` and
`codex_notes_sync_spec.md`, then verify the design against the current
handlers, services, store methods, queries, and migrations.

## SQL, migrations, and generated code

- Edit SQL in `db/queries` or add a new numbered migration under
  `db/migrations`; do not hand-edit `db/generated`.
- After changing sqlc inputs, run `make sqlc` and inspect the generated diff.
- Keep migration `up` and `down` files paired. Do not rewrite an applied
  migration or silently change persisted data behavior without an explicit
  request and a documented risk assessment.
- Use `make migrate-up`, `make migrate-version`, and related targets for local
  migration work. `make db-reset` deletes the local PostgreSQL volume and
  requires explicit confirmation; never run it casually.
- Treat `.env`, `.dev/`, generated auth keys/tokens, and credentials as local
  secrets. Use `.env.example` for documented configuration and never commit
  real secrets.

## Go style and error boundaries

- Follow nearby Go naming, package, error-wrapping, and constructor patterns.
- Run `gofmt` on changed Go files. Keep comments concise and document
  non-obvious ownership, transaction, concurrency, lifecycle, or failure
  behavior when adding or changing code.
- Return consistent API errors through `internal/httpapi`; do not leak
  implementation details or use ad-hoc response envelopes.
- Preserve graceful shutdown behavior for HTTP servers, PostgreSQL pools,
  WebSocket listeners, hubs, and worker loops.
- Do not add a UI-specific architecture or frontend instructions: this is a
  backend repository.

## Verification

Choose the narrowest checks that cover the change, expanding them when risk
requires:

- Formatting: `gofmt -w <changed-go-files>` or the equivalent editor action.
- Unit/package tests: `go test ./...` or `make test`.
- Concurrency-sensitive changes, especially realtime or worker code:
  `go test -race ./...` or `make test-race`.
- Static analysis: `go vet ./...` or `make lint`.
- Build/entrypoint changes: `go build ./...`.
- Integration/database changes, when PostgreSQL and `TEST_DATABASE_URL` are
  available: `make test-integration`.
- SQL generation changes: `make sqlc`, followed by tests or a build as
  appropriate.

Do not claim a check passed if it was skipped, unavailable, or failed. Report
the exact command, result, and any environment limitation. Honor `/no-test`
when active.

## Review and handoff

- Do not commit, push, deploy, modify credentials, delete user data, or make
  unrelated external changes unless the user explicitly requests it.
- Treat current source, tests, migrations, generated output, and direct tool
  results as stronger evidence than stale notes or examples.
- Finish with a concise summary of changed files, verification performed,
  assumptions, skipped checks, failures, and unresolved risks.

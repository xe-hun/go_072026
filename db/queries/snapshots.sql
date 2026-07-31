-- name: GetLatestSnapshotForNote :one
SELECT *
FROM note_snapshots
WHERE note_id = $1
  AND owner_id = $2
ORDER BY version DESC
LIMIT 1;

-- name: InsertSnapshot :one
INSERT INTO note_snapshots (
    id, note_id, owner_id, version, snapshot_format, schema_version, snapshot_data, checksum
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (note_id, version) DO UPDATE
SET snapshot_data = EXCLUDED.snapshot_data,
    checksum = EXCLUDED.checksum
RETURNING *;

-- name: EnqueueOutboxJob :one
INSERT INTO outbox_jobs (job_type, payload, available_at)
VALUES ($1, $2, COALESCE($3, now()))
RETURNING *;

-- name: ClaimOutboxJob :one
WITH candidate AS (
    SELECT id
    FROM outbox_jobs
    WHERE completed_at IS NULL
      AND available_at <= now()
      AND (locked_at IS NULL OR locked_at < now() - make_interval(secs => $2))
    ORDER BY available_at ASC, id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE outbox_jobs j
SET locked_at = now(),
    locked_by = $1,
    attempts = attempts + 1
FROM candidate
WHERE j.id = candidate.id
RETURNING j.*;

-- name: CompleteOutboxJob :exec
UPDATE outbox_jobs
SET completed_at = now(),
    locked_at = NULL,
    locked_by = NULL,
    last_error = NULL
WHERE id = $1;

-- name: FailOutboxJob :exec
UPDATE outbox_jobs
SET locked_at = NULL,
    locked_by = NULL,
    last_error = $2,
    available_at = now() + make_interval(secs => $3)
WHERE id = $1;


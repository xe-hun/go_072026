-- name: FindProcessedOperation :one
-- Looks up an operation by the idempotency key.
SELECT *
FROM note_changes
WHERE device_id = $1
  AND client_operation_id = $2;

-- name: InsertNoteChange :one
-- Appends one accepted operation to sync history.
INSERT INTO note_changes (
    id,
    owner_id,
    note_id,
    block_id,
    device_id,
    client_operation_id,
    operation_type,
    base_note_version,
    resulting_note_version,
    change_format,
    schema_version,
    change_data
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetChangesAfterCursor :many
-- Pulls remote changes after a cursor, excluding the calling device.
SELECT *
FROM note_changes
WHERE owner_id = $1
  AND global_sequence > $2
  AND device_id <> $3
ORDER BY global_sequence ASC
LIMIT $4;

-- name: CountChangesSinceLastSnapshot :one
-- Counts unsnapshotted changes for snapshot threshold checks.
SELECT COUNT(*)::bigint
FROM note_changes c
WHERE c.note_id = $1
  AND c.resulting_note_version > COALESCE((
      SELECT MAX(version)
      FROM note_snapshots
      WHERE note_id = $1
  ), 0);

-- name: SumChangeBytesSinceLastSnapshot :one
-- Sums unsnapshotted change payload bytes for snapshot threshold checks.
SELECT COALESCE(SUM(octet_length(change_data::text)), 0)::bigint
FROM note_changes c
WHERE c.note_id = $1
  AND c.resulting_note_version > COALESCE((
      SELECT MAX(version)
      FROM note_snapshots
      WHERE note_id = $1
  ), 0);

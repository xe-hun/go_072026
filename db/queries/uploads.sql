-- name: CreateFileUpload :one
INSERT INTO file_uploads (
    id, note_id, owner_id, block_id, original_filename, storage_bucket,
    storage_path, content_type, expected_size, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetFileUploadForOwner :one
SELECT *
FROM file_uploads
WHERE id = $1
  AND owner_id = $2;

-- name: CountPendingFileUploads :one
SELECT count(*)
FROM file_uploads
WHERE owner_id = $1
  AND status = 'PENDING'
  AND expires_at > now();

-- name: MarkFileUploadFailed :one
UPDATE file_uploads
SET status = 'FAILED',
    error_message = $3,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND status = 'PENDING'
RETURNING *;

-- name: CompleteFileUpload :one
UPDATE file_uploads
SET status = 'COMPLETED',
    actual_size = $3,
    verified_content_type = $4,
    error_message = NULL,
    completed_at = now(),
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND status = 'PENDING'
RETURNING *;

-- name: MarkFileUploadDeleted :one
UPDATE file_uploads
SET status = 'DELETED',
    error_message = NULL,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND status <> 'DELETED'
RETURNING *;

-- name: MarkFileUploadCancelled :one
UPDATE file_uploads
SET status = 'CANCELLED',
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND status <> 'DELETED'
RETURNING *;

-- name: ListExpiredFileUploads :many
SELECT *
FROM file_uploads
WHERE status = 'PENDING'
  AND expires_at < $1
ORDER BY expires_at ASC
LIMIT $2;

-- name: MarkFileUploadExpired :one
UPDATE file_uploads
SET status = 'EXPIRED',
    error_message = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'PENDING'
RETURNING *;

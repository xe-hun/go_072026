-- name: GetNoteForOwner :one
SELECT *
FROM notes
WHERE id = $1
  AND owner_id = $2;

-- name: GetNoteForOwnerForUpdate :one
SELECT *
FROM notes
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: ListBlocksForNote :many
SELECT *
FROM note_blocks
WHERE note_id = $1
ORDER BY position ASC, created_at ASC;

-- name: GetBlockForNoteForUpdate :one
SELECT *
FROM note_blocks
WHERE id = $1
  AND note_id = $2
FOR UPDATE;

-- name: CreateNote :one
INSERT INTO notes (
    id, owner_id, category_id, title, metadata, current_version
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpdateNoteState :one
UPDATE notes
SET category_id = $3,
    title = $4,
    metadata = $5,
    current_version = $6,
    updated_at = now(),
    deleted_at = $7
WHERE id = $1
  AND owner_id = $2
RETURNING *;

-- name: CreateBlock :one
INSERT INTO note_blocks (
    id, note_id, block_type, text_content, position, properties, current_version
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: UpdateBlockState :one
UPDATE note_blocks
SET block_type = $3,
    text_content = $4,
    position = $5,
    properties = $6,
    current_version = $7,
    updated_at = now(),
    deleted_at = $8
WHERE id = $1
  AND note_id = $2
RETURNING *;


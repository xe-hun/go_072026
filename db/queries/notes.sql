-- name: GetNoteForOwner :one
-- Reads one note with owner scoping.
SELECT *
FROM notes
WHERE id = $1
  AND owner_id = $2;

-- name: GetNoteForOwnerForUpdate :one
-- Reads and locks the complete note before applying a note batch.
SELECT *
FROM notes
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: ListBlocksForNote :many
-- Returns all blocks for a note in fractional position order.
SELECT *
FROM note_blocks
WHERE note_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateNote :exec
-- Inserts current note state.
INSERT INTO notes (
    id, owner_id, title, metadata, current_version
) VALUES (
    $1, $2, $3, $4, $5
);

-- name: UpdateNoteState :exec
-- Writes the complete note state after a successful operation batch.
UPDATE notes
SET title = $3,
    metadata = $4,
    current_version = $5,
    updated_at = now(),
    deleted_at = $6
WHERE id = $1
  AND owner_id = $2;

-- name: CreateBlock :exec
-- Inserts current block state.
INSERT INTO note_blocks (
    id, note_id, block_type, text_content, position, properties
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: UpdateBlockState :exec
-- Writes complete block state after a successful operation batch.
UPDATE note_blocks
SET block_type = $3,
    text_content = $4,
    position = $5,
    properties = $6,
    updated_at = now(),
    deleted_at = $7
WHERE id = $1
  AND note_id = $2;

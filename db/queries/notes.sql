-- name: GetNoteForOwner :one
-- Reads one note with owner scoping.
SELECT *
FROM notes
WHERE id = $1
  AND owner_id = $2;

-- name: GetNoteVersionForOwnerForUpdate :one
-- Reads and locks only the note version before a mutation.
SELECT current_version
FROM notes
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: GetNoteMetadataForOwnerForUpdate :one
-- Reads only metadata and the version for a metadata mutation.
SELECT metadata, current_version
FROM notes
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: GetNoteTitleForOwnerForUpdate :one
-- Reads only the title and version for a title mutation.
SELECT title, current_version
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

-- name: GetBlockForNoteForUpdate :one
-- Reads and locks only the block identity for a delete.
SELECT id
FROM note_blocks
WHERE id = $1
  AND note_id = $2
  AND position = $3
FOR UPDATE;

-- name: GetBlockPropertiesForUpdate :one
-- Reads only properties for a block property mutation.
SELECT properties
FROM note_blocks
WHERE id = $1
  AND note_id = $2
FOR UPDATE;

-- name: GetBlockTextForUpdate :one
-- Reads only text for a block text mutation.
SELECT text_content
FROM note_blocks
WHERE id = $1
  AND note_id = $2
FOR UPDATE;

-- name: CreateNote :exec
-- Inserts current note state.
INSERT INTO notes (
    id, owner_id, title, metadata, current_version
) VALUES (
    $1, $2, $3, $4, $5
);

-- name: UpdateNoteMetadata :exec
-- Updates only note metadata and the already-validated version.
UPDATE notes
SET metadata = $3,
    current_version = $4,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2;

-- name: UpdateNoteTitle :exec
-- Updates only note title and the already-validated version.
UPDATE notes
SET title = $3,
    current_version = $4,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2;

-- name: IncrementNoteVersion :exec
-- Increments the parent note version without loading its other columns.
UPDATE notes
SET current_version = $3,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2;

-- name: DeleteNote :exec
-- Soft deletes a note and updates only the fields needed by the operation.
UPDATE notes
SET current_version = $3,
    updated_at = now(),
    deleted_at = now()
WHERE id = $1
  AND owner_id = $2;

-- name: CreateBlock :exec
-- Inserts current block state.
INSERT INTO note_blocks (
    id, note_id, block_type, text_content, position, properties
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: UpdateBlockProperties :exec
-- Updates only block properties.
UPDATE note_blocks
SET properties = $3,
    updated_at = now()
WHERE id = $1
  AND note_id = $2;

-- name: UpdateBlockText :exec
-- Updates only block text.
UPDATE note_blocks
SET text_content = $3,
    updated_at = now()
WHERE id = $1
  AND note_id = $2;

-- name: DeleteBlock :exec
-- Soft deletes a block.
UPDATE note_blocks
SET updated_at = now(),
    deleted_at = now()
WHERE id = $1
  AND note_id = $2;

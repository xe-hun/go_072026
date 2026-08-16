-- name: CreateCategory :exec
-- Inserts a user-owned category.
INSERT INTO categories (id, owner_id, name)
VALUES ($1, $2, $3);

-- name: GetCategoryForOwnerForUpdate :one
-- Locks only the category identity before a mutation.
SELECT id
FROM categories
WHERE id = $1
  AND owner_id = $2
  AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateCategory :exec
-- Renames an active category.
UPDATE categories
SET name = $3,
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND deleted_at IS NULL;

-- name: DeleteCategory :exec
-- Soft deletes an active category.
UPDATE categories
SET updated_at = now(),
    deleted_at = now()
WHERE id = $1
  AND owner_id = $2
  AND deleted_at IS NULL;

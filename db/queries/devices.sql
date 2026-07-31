-- name: CreateDevice :one
INSERT INTO sync_devices (
    id, owner_id, device_name, platform, app_version, protocol_version
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListDevices :many
SELECT *
FROM sync_devices
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetDeviceForOwner :one
SELECT *
FROM sync_devices
WHERE id = $1
  AND owner_id = $2;

-- name: GetDeviceForOwnerForUpdate :one
SELECT *
FROM sync_devices
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: RevokeDevice :exec
UPDATE sync_devices
SET revoked_at = now(),
    last_seen_at = now()
WHERE id = $1
  AND owner_id = $2
  AND revoked_at IS NULL;

-- name: UpdateDeviceCursor :exec
UPDATE sync_devices
SET last_global_cursor = GREATEST(last_global_cursor, $3),
    last_seen_at = now()
WHERE id = $1
  AND owner_id = $2;


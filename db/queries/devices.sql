-- name: CreateDevice :one
-- Inserts a user-owned sync device and returns its stored state.
INSERT INTO sync_devices (
    id, owner_id, device_name, platform, app_version, protocol_version
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListDevices :many
-- Returns all devices for a user, including revoked devices.
SELECT *
FROM sync_devices
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetDeviceForOwner :one
-- Fetches one device with owner scoping.
SELECT *
FROM sync_devices
WHERE id = $1
  AND owner_id = $2;

-- name: GetDeviceForOwnerForUpdate :one
-- Fetches and locks one device for cursor/last_seen updates.
SELECT *
FROM sync_devices
WHERE id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: RevokeDevice :exec
-- Soft-revokes a device so future sync calls are rejected.
UPDATE sync_devices
SET revoked_at = now(),
    last_seen_at = now()
WHERE id = $1
  AND owner_id = $2
  AND revoked_at IS NULL;

-- name: UpdateDeviceCursor :exec
-- Advances the cursor without allowing it to move backwards.
UPDATE sync_devices
SET last_global_cursor = GREATEST(last_global_cursor, $3),
    last_seen_at = now()
WHERE id = $1
  AND owner_id = $2;

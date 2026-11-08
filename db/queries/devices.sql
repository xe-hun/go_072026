-- name: CreateDevice :one
-- Inserts a user-owned sync device and returns its stored state.
INSERT INTO sync_devices (
    device_id, owner_id, device_name, platform, app_version, protocol_version
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
WHERE device_id = $1
  AND owner_id = $2;

-- name: GetDeviceForOwnerForUpdate :one
-- Fetches and locks one device for sync/last_seen updates.
SELECT *
FROM sync_devices
WHERE device_id = $1
  AND owner_id = $2
FOR UPDATE;

-- name: RevokeDevice :exec
-- Soft-revokes a device so future sync calls are rejected.
UPDATE sync_devices
SET revoked_at = now(),
    last_seen_at = now()
WHERE device_id = $1
  AND owner_id = $2
  AND revoked_at IS NULL;

-- name: TouchDevice :exec
-- Updates device activity after a successful sync.
UPDATE sync_devices
SET last_seen_at = now()
WHERE device_id = $1
  AND owner_id = $2;

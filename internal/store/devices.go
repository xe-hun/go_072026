package store

import (
	"context"

	"github.com/google/uuid"

	db "notes-server/db/generated"
)

// CreateDevice inserts a new sync_devices row and returns the stored device.
func (s *Store) CreateDevice(ctx context.Context, arg CreateDeviceParams) (SyncDevice, error) {
	device, err := s.q.CreateDevice(ctx, db.CreateDeviceParams{
		ID:              pgUUID(arg.ID),
		OwnerID:         pgUUID(arg.OwnerID),
		DeviceName:      pgText(arg.DeviceName),
		Platform:        pgText(arg.Platform),
		AppVersion:      pgText(arg.AppVersion),
		ProtocolVersion: arg.ProtocolVersion,
	})
	return fromDBDevice(device), err
}

// ListDevices returns all devices for one owner.
func (s *Store) ListDevices(ctx context.Context, ownerID uuid.UUID) ([]SyncDevice, error) {
	rows, err := s.q.ListDevices(ctx, pgUUID(ownerID))
	if err != nil {
		return nil, err
	}
	devices := make([]SyncDevice, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, fromDBDevice(row))
	}
	return devices, nil
}

// GetDeviceForOwner fetches a device only when it belongs to the owner.
func (s *Store) GetDeviceForOwner(ctx context.Context, deviceID, ownerID uuid.UUID) (SyncDevice, error) {
	device, err := s.q.GetDeviceForOwner(ctx, db.GetDeviceForOwnerParams{
		ID:      pgUUID(deviceID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBDevice(device), mapNoRows(err)
}

// GetDeviceForOwnerForUpdate locks the device row. Sync uses this to serialize
// cursor/last_seen updates for a device.
func (s *Store) GetDeviceForOwnerForUpdate(ctx context.Context, deviceID, ownerID uuid.UUID) (SyncDevice, error) {
	device, err := s.q.GetDeviceForOwnerForUpdate(ctx, db.GetDeviceForOwnerForUpdateParams{
		ID:      pgUUID(deviceID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBDevice(device), mapNoRows(err)
}

// RevokeDevice soft-revokes a device by setting revoked_at.
func (s *Store) RevokeDevice(ctx context.Context, deviceID, ownerID uuid.UUID) error {
	// sqlc's exec helper does not report rows affected, so fetch first to map a
	// missing device into ErrNotFound.
	if _, err := s.GetDeviceForOwner(ctx, deviceID, ownerID); err != nil {
		return err
	}
	return s.q.RevokeDevice(ctx, db.RevokeDeviceParams{ID: pgUUID(deviceID), OwnerID: pgUUID(ownerID)})
}

// UpdateDeviceCursor advances a device cursor without allowing it to move
// backwards.
func (s *Store) UpdateDeviceCursor(ctx context.Context, deviceID, ownerID uuid.UUID, cursor int64) error {
	return s.q.UpdateDeviceCursor(ctx, db.UpdateDeviceCursorParams{
		ID:               pgUUID(deviceID),
		OwnerID:          pgUUID(ownerID),
		LastGlobalCursor: cursor,
	})
}

package store

import (
	"context"

	"github.com/google/uuid"

	db "notes-server/db/generated"
)

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

func (s *Store) GetDeviceForOwner(ctx context.Context, deviceID, ownerID uuid.UUID) (SyncDevice, error) {
	device, err := s.q.GetDeviceForOwner(ctx, db.GetDeviceForOwnerParams{
		ID:      pgUUID(deviceID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBDevice(device), mapNoRows(err)
}

func (s *Store) GetDeviceForOwnerForUpdate(ctx context.Context, deviceID, ownerID uuid.UUID) (SyncDevice, error) {
	device, err := s.q.GetDeviceForOwnerForUpdate(ctx, db.GetDeviceForOwnerForUpdateParams{
		ID:      pgUUID(deviceID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBDevice(device), mapNoRows(err)
}

func (s *Store) RevokeDevice(ctx context.Context, deviceID, ownerID uuid.UUID) error {
	if _, err := s.GetDeviceForOwner(ctx, deviceID, ownerID); err != nil {
		return err
	}
	return s.q.RevokeDevice(ctx, db.RevokeDeviceParams{ID: pgUUID(deviceID), OwnerID: pgUUID(ownerID)})
}

func (s *Store) UpdateDeviceCursor(ctx context.Context, deviceID, ownerID uuid.UUID, cursor int64) error {
	return s.q.UpdateDeviceCursor(ctx, db.UpdateDeviceCursorParams{
		ID:               pgUUID(deviceID),
		OwnerID:          pgUUID(ownerID),
		LastGlobalCursor: cursor,
	})
}

package devices

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Service contains device business rules. It scopes every operation to the
// authenticated owner ID supplied by the handler.
type Service struct {
	store *store.Store
}

// NewService wires the device service to the persistence boundary.
func NewService(store *store.Store) *Service {
	return &Service{store: store}
}

// Register validates protocol compatibility and creates a user-owned sync
// device.
func (s *Service) Register(ctx context.Context, ownerID uuid.UUID, req RegisterDeviceRequest) (DeviceResponse, error) {
	var model Device
	if err := model.FromRequest(req); err != nil {
		return DeviceResponse{}, err
	}
	params, err := model.Entity(ownerID)
	if err != nil {
		return DeviceResponse{}, err
	}
	device, err := s.store.CreateDevice(ctx, params)
	if err != nil {
		return DeviceResponse{}, err
	}
	var response DeviceResponse
	if err := response.FromEntity(device); err != nil {
		return DeviceResponse{}, err
	}
	return response, nil
}

// List returns all devices owned by the authenticated user, including revoked
// devices for audit/debug visibility.
func (s *Service) List(ctx context.Context, ownerID uuid.UUID) (ListDevicesResponse, error) {
	devices, err := s.store.ListDevices(ctx, ownerID)
	if err != nil {
		return ListDevicesResponse{}, err
	}
	var response ListDevicesResponse
	if err := response.FromEntities(devices); err != nil {
		return ListDevicesResponse{}, err
	}
	return response, nil
}

// Revoke marks a device as revoked. Future sync attempts from that device are
// rejected by the sync service.
func (s *Service) Revoke(ctx context.Context, ownerID, deviceID uuid.UUID) error {
	err := s.store.RevokeDevice(ctx, deviceID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return httpapi.NotFound("Device not found.")
	}
	return err
}

package devices

import (
	"context"
	"errors"
	"net/http"

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
	protocolVersion := req.ProtocolVersion
	if protocolVersion == 0 {
		// Protocol v1 is the initial default so older clients can omit the field.
		protocolVersion = 1
	}
	if protocolVersion != 1 {
		return DeviceResponse{}, httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	deviceID := uuid.New()
	if req.DeviceID != nil {
		// Client-generated IDs support offline-first clients that create device
		// records before the first successful network call.
		deviceID = *req.DeviceID
	}
	device, err := s.store.CreateDevice(ctx, store.CreateDeviceParams{
		ID:              deviceID,
		OwnerID:         ownerID,
		DeviceName:      req.DeviceName,
		Platform:        req.Platform,
		AppVersion:      req.AppVersion,
		ProtocolVersion: protocolVersion,
	})
	if err != nil {
		return DeviceResponse{}, err
	}
	return mapDevice(device), nil
}

// List returns all devices owned by the authenticated user, including revoked
// devices for audit/debug visibility.
func (s *Service) List(ctx context.Context, ownerID uuid.UUID) (ListDevicesResponse, error) {
	devices, err := s.store.ListDevices(ctx, ownerID)
	if err != nil {
		return ListDevicesResponse{}, err
	}
	resp := ListDevicesResponse{Devices: make([]DeviceResponse, 0, len(devices))}
	for _, device := range devices {
		resp.Devices = append(resp.Devices, mapDevice(device))
	}
	return resp, nil
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

// mapDevice converts nullable database values into pointer JSON fields.
func mapDevice(device store.SyncDevice) DeviceResponse {
	return DeviceResponse{
		ID:               device.ID,
		DeviceName:       store.TextPtr(device.DeviceName),
		Platform:         store.TextPtr(device.Platform),
		AppVersion:       store.TextPtr(device.AppVersion),
		ProtocolVersion:  device.ProtocolVersion,
		LastGlobalCursor: device.LastGlobalCursor,
		LastSeenAt:       device.LastSeenAt,
		CreatedAt:        device.CreatedAt,
		RevokedAt:        store.TimePtr(device.RevokedAt),
	}
}

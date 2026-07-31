package devices

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

type Service struct {
	store *store.Store
}

func NewService(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Register(ctx context.Context, ownerID uuid.UUID, req RegisterDeviceRequest) (DeviceResponse, error) {
	protocolVersion := req.ProtocolVersion
	if protocolVersion == 0 {
		protocolVersion = 1
	}
	if protocolVersion != 1 {
		return DeviceResponse{}, httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	deviceID := uuid.New()
	if req.DeviceID != nil {
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

func (s *Service) Revoke(ctx context.Context, ownerID, deviceID uuid.UUID) error {
	err := s.store.RevokeDevice(ctx, deviceID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return httpapi.NotFound("Device not found.")
	}
	return err
}

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

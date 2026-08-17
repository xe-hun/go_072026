package devices

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Device is the device model used between HTTP requests, persistence entities,
// and API responses. It contains normalized values only.
type Device struct {
	ID               uuid.UUID
	OwnerID          uuid.UUID
	DeviceName       *string
	Platform         *string
	AppVersion       *string
	ProtocolVersion  int32
	LastGlobalCursor int64
	LastSeenAt       time.Time
	CreatedAt        time.Time
	RevokedAt        *time.Time
}

// FromRequest builds a device model from the registration request and applies
// request-level defaults and validation.
func (m *Device) FromRequest(req RegisterDeviceRequest) error {
	if req.DeviceID != nil && *req.DeviceID == uuid.Nil {
		return httpapi.InvalidRequest("deviceId must be a valid UUID.")
	}

	protocolVersion := req.ProtocolVersion
	if protocolVersion == 0 {
		protocolVersion = 1
	}
	if protocolVersion != 1 {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}

	m.ID = uuid.New()
	if req.DeviceID != nil {
		m.ID = *req.DeviceID
	}
	m.DeviceName = req.DeviceName
	m.Platform = req.Platform
	m.AppVersion = req.AppVersion
	m.ProtocolVersion = protocolVersion
	return nil
}

// FromEntity maps a persistence entity into the device model.
func (m *Device) FromEntity(entity store.SyncDevice) error {
	if entity.ID == uuid.Nil || entity.OwnerID == uuid.Nil {
		return errors.New("device entity must contain valid identifiers")
	}
	if entity.ProtocolVersion != 1 {
		return errors.New("device entity has an unsupported protocol version")
	}

	m.ID = entity.ID
	m.OwnerID = entity.OwnerID
	m.DeviceName = store.TextPtr(entity.DeviceName)
	m.Platform = store.TextPtr(entity.Platform)
	m.AppVersion = store.TextPtr(entity.AppVersion)
	m.ProtocolVersion = entity.ProtocolVersion
	m.LastGlobalCursor = entity.LastGlobalCursor
	m.LastSeenAt = entity.LastSeenAt
	m.CreatedAt = entity.CreatedAt
	m.RevokedAt = store.TimePtr(entity.RevokedAt)
	return nil
}

// Entity converts the model into the persistence input for device creation.
func (m Device) Entity(ownerID uuid.UUID) (store.CreateDeviceParams, error) {
	if ownerID == uuid.Nil || m.ID == uuid.Nil {
		return store.CreateDeviceParams{}, httpapi.InvalidRequest("device identifiers are required.")
	}
	if m.ProtocolVersion != 1 {
		return store.CreateDeviceParams{}, httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	return store.CreateDeviceParams{
		ID:              m.ID,
		OwnerID:         ownerID,
		DeviceName:      m.DeviceName,
		Platform:        m.Platform,
		AppVersion:      m.AppVersion,
		ProtocolVersion: m.ProtocolVersion,
	}, nil
}

// Response converts the model into the public API representation.
func (m Device) Response() DeviceResponse {
	return DeviceResponse{
		ID:               m.ID,
		DeviceName:       m.DeviceName,
		Platform:         m.Platform,
		AppVersion:       m.AppVersion,
		ProtocolVersion:  m.ProtocolVersion,
		LastGlobalCursor: m.LastGlobalCursor,
		LastSeenAt:       m.LastSeenAt,
		CreatedAt:        m.CreatedAt,
		RevokedAt:        m.RevokedAt,
	}
}

// FromEntity maps a persistence entity directly into the API response.
func (r *DeviceResponse) FromEntity(entity store.SyncDevice) error {
	var model Device
	if err := model.FromEntity(entity); err != nil {
		return err
	}
	*r = model.Response()
	return nil
}

// FromEntities maps persistence entities into a response list.
func (r *ListDevicesResponse) FromEntities(entities []store.SyncDevice) error {
	r.Devices = make([]DeviceResponse, 0, len(entities))
	for _, entity := range entities {
		var response DeviceResponse
		if err := response.FromEntity(entity); err != nil {
			return err
		}
		r.Devices = append(r.Devices, response)
	}
	return nil
}

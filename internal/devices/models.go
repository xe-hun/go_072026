package devices

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	constants "notes-server/internal"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Device is the device model used between HTTP requests, persistence entities,
// and API responses.
type Device struct {
	DeviceID         uuid.UUID
	DeviceIdentifier string
	OwnerID          uuid.UUID
	DeviceName       string
	Platform         *string
	AppVersion       *string
	ProtocolVersion  int32
	LastSeenAt       time.Time
	CreatedAt        time.Time
	RevokedAt        *time.Time
}

// FromRequest builds a device model from the registration request and applies
// request-level defaults and validation.
func (m *Device) FromRequest(req RegisterDeviceRequest) error {
	if req.DeviceID == uuid.Nil {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceIdentifier(req.DeviceIdentifier) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceIdentifier + " is required and must be 100 characters or fewer.")
	}
	if req.DeviceName == nil || *req.DeviceName == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceName + " is required.")
	}

	protocolVersion := req.ProtocolVersion
	if protocolVersion == 0 {
		protocolVersion = 1
	}
	if protocolVersion != 1 {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}

	m.DeviceID = req.DeviceID
	m.DeviceIdentifier = req.DeviceIdentifier
	m.DeviceName = *req.DeviceName
	m.Platform = req.Platform
	m.AppVersion = req.AppVersion
	m.ProtocolVersion = protocolVersion
	return nil
}

// FromEntity maps a persistence entity into the device model.
func (m *Device) FromEntity(entity store.SyncDevice) error {
	if entity.DeviceID == uuid.Nil || entity.OwnerID == uuid.Nil || !constants.ValidDeviceIdentifier(entity.DeviceIdentifier) || entity.DeviceName == "" {
		return errors.New("device entity must contain valid identifiers")
	}
	if entity.ProtocolVersion != 1 {
		return errors.New("device entity has an unsupported protocol version")
	}

	m.DeviceID = entity.DeviceID
	m.DeviceIdentifier = entity.DeviceIdentifier
	m.OwnerID = entity.OwnerID
	m.DeviceName = entity.DeviceName
	m.Platform = store.TextPtr(entity.Platform)
	m.AppVersion = store.TextPtr(entity.AppVersion)
	m.ProtocolVersion = entity.ProtocolVersion
	m.LastSeenAt = entity.LastSeenAt
	m.CreatedAt = entity.CreatedAt
	m.RevokedAt = store.TimePtr(entity.RevokedAt)
	return nil
}

// Entity converts the model into the persistence input for device creation.
func (m Device) Entity(ownerID uuid.UUID) (store.CreateDeviceParams, error) {
	if ownerID == uuid.Nil || m.DeviceID == uuid.Nil || !constants.ValidDeviceIdentifier(m.DeviceIdentifier) || m.DeviceName == "" {
		return store.CreateDeviceParams{}, httpapi.InvalidRequest("device identifiers are required.")
	}
	if m.ProtocolVersion != 1 {
		return store.CreateDeviceParams{}, httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	return store.CreateDeviceParams{
		DeviceID:         m.DeviceID,
		DeviceIdentifier: m.DeviceIdentifier,
		OwnerID:          ownerID,
		DeviceName:       m.DeviceName,
		Platform:         m.Platform,
		AppVersion:       m.AppVersion,
		ProtocolVersion:  m.ProtocolVersion,
	}, nil
}

// Response converts the model into the public API representation.
func (m Device) Response() DeviceResponse {
	return DeviceResponse{
		ID:               m.DeviceID,
		DeviceIdentifier: m.DeviceIdentifier,
		DeviceName:       m.DeviceName,
		Platform:         m.Platform,
		AppVersion:       m.AppVersion,
		ProtocolVersion:  m.ProtocolVersion,
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

func nextDeviceName(base string, existing []string) string {
	prefix := base + "-"
	maxSuffix := 0
	for _, name := range existing {
		if name == base {
			return base
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		suffix, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err == nil && suffix > maxSuffix {
			maxSuffix = suffix
		}
	}
	return fmt.Sprintf("%s-%d", base, maxSuffix+1)
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

package devices

import (
	"time"

	"github.com/google/uuid"
)

// RegisterDeviceRequest is the JSON body for POST /v1/devices.
type RegisterDeviceRequest struct {
	// DeviceID lets a client provide a stable UUID. When omitted, the server
	// generates one.
	DeviceID *uuid.UUID `json:"deviceId,omitempty"`
	// DeviceName is an optional human-readable label.
	DeviceName *string `json:"deviceName,omitempty"`
	// Platform identifies the client platform, for example ios, android, or web.
	Platform *string `json:"platform,omitempty"`
	// AppVersion records the client app build/version for diagnostics.
	AppVersion *string `json:"appVersion,omitempty"`
	// ProtocolVersion defaults to 1 and must match the supported sync protocol.
	ProtocolVersion int32 `json:"protocolVersion,omitempty"`
}

// DeviceResponse is the API-safe representation of a sync device.
type DeviceResponse struct {
	// ID is the device UUID used by sync requests.
	ID uuid.UUID `json:"id"`
	// DeviceName mirrors the optional display name supplied at registration.
	DeviceName *string `json:"deviceName,omitempty"`
	// Platform mirrors the optional platform supplied at registration.
	Platform *string `json:"platform,omitempty"`
	// AppVersion mirrors the optional app version supplied at registration.
	AppVersion *string `json:"appVersion,omitempty"`
	// ProtocolVersion is the sync protocol this device registered with.
	ProtocolVersion int32 `json:"protocolVersion"`
	// LastGlobalCursor is the furthest global change sequence observed by this
	// device.
	LastGlobalCursor int64 `json:"lastGlobalCursor"`
	// LastSeenAt is refreshed by sync and cursor updates.
	LastSeenAt time.Time `json:"lastSeenAt"`
	// CreatedAt records registration time.
	CreatedAt time.Time `json:"createdAt"`
	// RevokedAt is present after the device has been revoked.
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// ListDevicesResponse wraps device lists so the response can grow later without
// changing the top-level JSON type.
type ListDevicesResponse struct {
	// Devices contains the authenticated user's devices.
	Devices []DeviceResponse `json:"devices"`
}

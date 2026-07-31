package devices

import (
	"time"

	"github.com/google/uuid"
)

type RegisterDeviceRequest struct {
	DeviceID        *uuid.UUID `json:"deviceId,omitempty"`
	DeviceName      *string    `json:"deviceName,omitempty"`
	Platform        *string    `json:"platform,omitempty"`
	AppVersion      *string    `json:"appVersion,omitempty"`
	ProtocolVersion int32      `json:"protocolVersion,omitempty"`
}

type DeviceResponse struct {
	ID               uuid.UUID  `json:"id"`
	DeviceName       *string    `json:"deviceName,omitempty"`
	Platform         *string    `json:"platform,omitempty"`
	AppVersion       *string    `json:"appVersion,omitempty"`
	ProtocolVersion  int32      `json:"protocolVersion"`
	LastGlobalCursor int64      `json:"lastGlobalCursor"`
	LastSeenAt       time.Time  `json:"lastSeenAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
}

type ListDevicesResponse struct {
	Devices []DeviceResponse `json:"devices"`
}

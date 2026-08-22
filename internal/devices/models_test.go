package devices

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeviceFromRequestNormalizesDefaultsAndBuildsEntity(t *testing.T) {
	deviceID := "laptop-main"
	ownerID := uuid.New()
	model := Device{}
	if err := model.FromRequest(RegisterDeviceRequest{DeviceID: deviceID}); err != nil {
		t.Fatal(err)
	}
	entity, err := model.Entity(ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if entity.ID != deviceID || entity.OwnerID != ownerID || entity.ProtocolVersion != 1 {
		t.Fatalf("unexpected entity: %+v", entity)
	}
}

func TestDeviceFromRequestRejectsMissingOrLongDeviceID(t *testing.T) {
	for _, deviceID := range []string{"", "1234567890123456789012345678901"} {
		if err := (&Device{}).FromRequest(RegisterDeviceRequest{DeviceID: deviceID}); err == nil {
			t.Fatalf("expected device ID %q to fail", deviceID)
		}
	}
}

func TestDeviceFromRequestRejectsUnsupportedProtocol(t *testing.T) {
	if err := (&Device{}).FromRequest(RegisterDeviceRequest{DeviceID: "laptop-main", ProtocolVersion: 2}); err == nil {
		t.Fatal("expected unsupported protocol to fail")
	}
}

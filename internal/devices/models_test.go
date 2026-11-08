package devices

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeviceFromRequestAppliesDefaultsAndBuildsEntity(t *testing.T) {
	deviceID := uuid.New()
	ownerID := uuid.New()
	deviceName := "AndroidDevice"
	model := Device{}
	if err := model.FromRequest(RegisterDeviceRequest{
		DeviceID:         deviceID,
		DeviceIdentifier: "android-device",
		DeviceName:       &deviceName,
	}); err != nil {
		t.Fatal(err)
	}
	entity, err := model.Entity(ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if entity.DeviceID != deviceID || entity.DeviceIdentifier != "android-device" || entity.OwnerID != ownerID || entity.DeviceName != deviceName || entity.ProtocolVersion != 1 {
		t.Fatalf("unexpected entity: %+v", entity)
	}
}

func TestDeviceFromRequestRejectsMissingDeviceID(t *testing.T) {
	for _, deviceID := range []uuid.UUID{uuid.Nil} {
		if err := (&Device{}).FromRequest(RegisterDeviceRequest{DeviceID: deviceID}); err == nil {
			t.Fatalf("expected device ID %q to fail", deviceID)
		}
	}
}

func TestDeviceFromRequestRejectsUnsupportedProtocol(t *testing.T) {
	deviceName := "AndroidDevice"
	if err := (&Device{}).FromRequest(RegisterDeviceRequest{
		DeviceID:         uuid.New(),
		DeviceIdentifier: "android-device",
		DeviceName:       &deviceName,
		ProtocolVersion:  2,
	}); err == nil {
		t.Fatal("expected unsupported protocol to fail")
	}
}

func TestDeviceFromRequestRejectsMissingIdentifierOrName(t *testing.T) {
	deviceName := "AndroidDevice"
	for name, request := range map[string]RegisterDeviceRequest{
		"identifier": {DeviceID: uuid.New(), DeviceName: &deviceName},
		"name":       {DeviceID: uuid.New(), DeviceIdentifier: "android-device"},
		"null name":  {DeviceID: uuid.New(), DeviceIdentifier: "android-device", DeviceName: nil},
	} {
		if err := (&Device{}).FromRequest(request); err == nil {
			t.Fatalf("expected missing %s to fail", name)
		}
	}
}

func TestNextDeviceName(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		existing []string
		want     string
	}{
		{name: "first name", base: "AndroidDevice", want: "AndroidDevice-1"},
		{name: "existing base", base: "AndroidDevice", existing: []string{"AndroidDevice", "AndroidDevice-1"}, want: "AndroidDevice"},
		{name: "next suffix", base: "AndroidDevice", existing: []string{"AndroidDevice-1", "AndroidDevice-3"}, want: "AndroidDevice-4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextDeviceName(test.base, test.existing); got != test.want {
				t.Fatalf("nextDeviceName() = %q, want %q", got, test.want)
			}
		})
	}
}

// Package realtime contains the lightweight notification channel used by
// connected devices. It deliberately does not read notes or apply sync
// operations; it only tells a client that its normal sync endpoint may have
// newer data.
package realtime

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	constants "notes-server/internal"
)

const syncAvailableMessageType = "sync_available"

// SyncAvailableEvent is the small event exchanged between sync API and
// WebSocket server instances.
//
// Device IDs are strings in this project because they are client-supplied
// identifiers in sync_devices. The event carries no note data or operation
// payloads.
type SyncAvailableEvent struct {
	UserID         uuid.UUID `json:"userId"`
	OriginDeviceID string    `json:"originDeviceId"`
	LatestCursor   int64     `json:"latestCursor"`
}

// Validate rejects malformed broker events before they reach the local hub.
func (e SyncAvailableEvent) Validate() error {
	if e.UserID == uuid.Nil {
		return errors.New("sync available event user ID is required")
	}
	if !constants.ValidDeviceID(e.OriginDeviceID) {
		return errors.New("sync available event origin device ID is invalid")
	}
	if e.LatestCursor < 0 {
		return errors.New("sync available event cursor must be non-negative")
	}
	return nil
}

// Message is the only client-facing WebSocket message currently supported.
// payload is pre-encoded once per broker event and shared by all local
// clients, avoiding a JSON allocation for every connection.
type Message struct {
	Type         string `json:"type"`
	LatestCursor int64  `json:"latestCursor"`
	payload      []byte
}

// NewSyncAvailableMessage builds the tiny message sent to a client.
func NewSyncAvailableMessage(latestCursor int64) Message {
	message := Message{
		Type:         syncAvailableMessageType,
		LatestCursor: latestCursor,
	}
	message.payload, _ = json.Marshal(struct {
		Type         string `json:"type"`
		LatestCursor int64  `json:"latestCursor"`
	}{
		Type:         message.Type,
		LatestCursor: message.LatestCursor,
	})
	return message
}

// wirePayload returns the immutable payload written to a WebSocket. The
// fallback keeps Message useful in focused unit tests that construct it
// directly instead of using NewSyncAvailableMessage.
func (m Message) wirePayload() []byte {
	if len(m.payload) != 0 {
		return m.payload
	}
	payload, _ := json.Marshal(m)
	return payload
}

package syncapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProtocolVersion is the only sync protocol version accepted by this server.
const ProtocolVersion = 1

// Request is the JSON body accepted by POST /v1/sync.
type Request struct {
	// ProtocolVersion must match ProtocolVersion.
	ProtocolVersion int `json:"protocolVersion"`
	// ClientVersion is informational and useful in logs/debugging.
	ClientVersion string `json:"clientVersion,omitempty"`
	// DeviceID identifies the registered device making the request.
	DeviceID uuid.UUID `json:"deviceId"`
	// Cursor is the last global sequence the client has processed.
	Cursor int64 `json:"cursor"`
	// Limit bounds how many remote changes are pulled in this response.
	Limit int32 `json:"limit,omitempty"`
	// Operations are client-submitted writes to apply before pulling changes.
	Operations []ClientOperation `json:"operations"`
}

// ClientOperation is one idempotent note, block, or category mutation
// submitted by a device.
type ClientOperation struct {
	// OperationID is stable across retries and forms the idempotency key with
	// DeviceID.
	OperationID uuid.UUID `json:"operationId"`
	// NoteID is the note being created or mutated.
	NoteID uuid.UUID `json:"noteId"`
	// OperationType is the concrete operation name.
	OperationType string `json:"operationType"`
	// Sequence orders client operations within a note batch.
	Sequence int `json:"sequence"`
	// BaseNoteVersion is the version the client edited from.
	BaseNoteVersion int64 `json:"baseNoteVersion"`
	// ChangeFormat currently supports structured-operation-v1.
	ChangeFormat string `json:"changeFormat,omitempty"`
	// ChangeData contains changed fields for the operation.
	ChangeData json.RawMessage `json:"changeData"`
}

// Response is returned with HTTP 200 for valid sync batches, even when some
// individual operations are rejected.
type Response struct {
	// Accepted lists operations that mutated state or were idempotent replays.
	Accepted []AcceptedDTO `json:"accepted"`
	// Rejected lists per-operation validation/conflict failures.
	Rejected []RejectedDTO `json:"rejected"`
	// Changes contains remote changes after the supplied cursor.
	Changes []PulledChange `json:"changes"`
	// NextCursor is the cursor the client should store after applying this
	// response.
	NextCursor int64 `json:"nextCursor"`
	// HasMore tells the client to immediately pull again with NextCursor.
	HasMore bool `json:"hasMore"`
	// ResyncRequired is reserved for expired cursors/full resync flows.
	ResyncRequired bool `json:"resyncRequired"`
	// Reason describes why a resync is required.
	Reason string `json:"reason,omitempty"`
}

// AcceptedDTO reports the resulting version for an accepted note batch.
type AcceptedDTO struct {
	NoteID            uuid.UUID `json:"noteId"`
	ServerNoteVersion int64     `json:"serverNoteVersion"`
}

// RejectedDTO reports why one note batch in an otherwise valid sync request
// could not be applied.
type RejectedDTO struct {
	Code         string        `json:"code"`
	Message      string        `json:"message,omitempty"`
	NoteID       uuid.UUID     `json:"noteId,omitempty"`
	NoteSnapshot *NoteSnapshot `json:"noteSnapshot,omitempty"`
	// Client/server versions are included for conflict resolution UI.
	ClientNoteVersion int64 `json:"clientNoteVersion,omitempty"`
	ServerNoteVersion int64 `json:"serverNoteVersion,omitempty"`
}

// NoteSnapshot contains the current note and block state returned once for a
// rejected note batch so clients can resolve the conflict.
type NoteSnapshot struct {
	ID             uuid.UUID       `json:"id"`
	OwnerID        uuid.UUID       `json:"ownerId"`
	Title          string          `json:"title"`
	NoteProperties json.RawMessage `json:"noteProperties"`
	CurrentVersion int64           `json:"currentVersion"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
	Blocks         []BlockSnapshot `json:"blocks"`
}

// BlockSnapshot is one block inside a current note snapshot.
type BlockSnapshot struct {
	ID              uuid.UUID       `json:"id"`
	NoteID          uuid.UUID       `json:"noteId"`
	BlockType       string          `json:"blockType"`
	TextContent     string          `json:"textContent"`
	Position        string          `json:"position"`
	BlockProperties json.RawMessage `json:"blockProperties"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	DeletedAt       *time.Time      `json:"deletedAt,omitempty"`
}

// PulledChange is a change-log row sent to another device during pull sync.
type PulledChange struct {
	ID            uuid.UUID  `json:"id"`
	OperationID   uuid.UUID  `json:"operationId"`
	NoteID        uuid.UUID  `json:"noteId"`
	BlockID       *uuid.UUID `json:"blockId,omitempty"`
	DeviceID      uuid.UUID  `json:"deviceId"`
	OperationType string     `json:"operationType"`
	// Base/resulting versions let clients apply or inspect changes in order.
	BaseNoteVersion      int64           `json:"baseNoteVersion"`
	ResultingNoteVersion int64           `json:"resultingNoteVersion"`
	ChangeFormat         string          `json:"changeFormat"`
	SchemaVersion        int32           `json:"schemaVersion"`
	ChangeData           json.RawMessage `json:"changeData"`
	// Sequence is the global cursor value for pagination.
	Sequence  int64     `json:"sequence"`
	CreatedAt time.Time `json:"createdAt"`
}

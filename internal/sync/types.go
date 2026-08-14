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

// ClientOperation is one idempotent note or block mutation submitted by a
// device.
type ClientOperation struct {
	// OperationID is stable across retries and forms the idempotency key with
	// DeviceID.
	OperationID uuid.UUID `json:"operationId"`
	// NoteID is the note being created or mutated.
	NoteID uuid.UUID `json:"noteId"`
	// BlockID is required for block operations and omitted for note operations.
	BlockID *uuid.UUID `json:"blockId,omitempty"`
	// EntityType is "note" or "block".
	EntityType string `json:"entityType"`
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
	Accepted []AcceptedOperation `json:"accepted"`
	// Rejected lists per-operation validation/conflict failures.
	Rejected []RejectedOperation `json:"rejected"`
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

// AcceptedOperation reports authoritative versions/sequences for an accepted
// note batch.
type AcceptedOperation struct {
	OperationID  uuid.UUID   `json:"operationId"`
	OperationIDs []uuid.UUID `json:"operationIds,omitempty"`
	NoteID       uuid.UUID   `json:"noteId"`
	BlockID      *uuid.UUID  `json:"blockId,omitempty"`
	// NoteVersion is the resulting note version.
	NoteVersion int64 `json:"noteVersion"`
	// Sequence is the global change sequence assigned by PostgreSQL.
	Sequence int64 `json:"sequence"`
}

// RejectedOperation reports why one note batch in an otherwise valid sync
// request could not be applied.
type RejectedOperation struct {
	OperationID  uuid.UUID         `json:"operationId"`
	OperationIDs []uuid.UUID       `json:"operationIds,omitempty"`
	Code         string            `json:"code"`
	Message      string            `json:"message,omitempty"`
	NoteID       uuid.UUID         `json:"noteId,omitempty"`
	BlockID      *uuid.UUID        `json:"blockId,omitempty"`
	NoteSnapshot *RejectedSnapshot `json:"noteSnapshot,omitempty"`
	// Client/server versions are included for conflict resolution UI.
	ClientNoteVersion int64 `json:"clientNoteVersion,omitempty"`
	ServerNoteVersion int64 `json:"serverNoteVersion,omitempty"`
}

// RejectedSnapshot is the current note state returned once for a rejected note
// batch so clients can reset conflict UI to the server-authoritative document.
type RejectedSnapshot struct {
	ID             uuid.UUID       `json:"id"`
	OwnerID        uuid.UUID       `json:"ownerId"`
	CategoryID     *uuid.UUID      `json:"categoryId,omitempty"`
	Title          string          `json:"title"`
	Metadata       json.RawMessage `json:"metadata"`
	CurrentVersion int64           `json:"currentVersion"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
	Blocks         []RejectedBlock `json:"blocks"`
}

// RejectedBlock is one block inside a rejected batch's current note snapshot.
type RejectedBlock struct {
	ID          uuid.UUID       `json:"id"`
	NoteID      uuid.UUID       `json:"noteId"`
	BlockType   string          `json:"blockType"`
	TextContent string          `json:"textContent"`
	Position    string          `json:"position"`
	Properties  json.RawMessage `json:"properties"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	DeletedAt   *time.Time      `json:"deletedAt,omitempty"`
}

// PulledChange is a change-log row sent to another device during pull sync.
type PulledChange struct {
	ID            uuid.UUID  `json:"id"`
	OperationID   uuid.UUID  `json:"operationId"`
	NoteID        uuid.UUID  `json:"noteId"`
	BlockID       *uuid.UUID `json:"blockId,omitempty"`
	DeviceID      uuid.UUID  `json:"deviceId"`
	EntityType    string     `json:"entityType"`
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

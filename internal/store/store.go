package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrNotFound is the store-level sentinel for missing rows. Services translate
// it into API-safe 404/403 responses depending on context.
var ErrNotFound = errors.New("not found")

// SyncDevice mirrors a row in sync_devices after conversion from sqlc types.
type SyncDevice struct {
	// ID is the device UUID used by sync requests.
	ID uuid.UUID
	// OwnerID scopes the device to one authenticated user.
	OwnerID uuid.UUID
	// DeviceName is nullable because clients may register anonymously named
	// devices.
	DeviceName pgtype.Text
	// Platform is optional client metadata.
	Platform pgtype.Text
	// AppVersion is optional client metadata.
	AppVersion pgtype.Text
	// ProtocolVersion records the sync protocol supported by this device.
	ProtocolVersion int32
	// LastGlobalCursor stores the largest global sequence observed by the device.
	LastGlobalCursor int64
	LastSeenAt       time.Time
	CreatedAt        time.Time
	// RevokedAt is valid after the device is revoked.
	RevokedAt pgtype.Timestamptz
}

// Note mirrors current note state from notes.
type Note struct {
	// ID is the note UUID.
	ID uuid.UUID
	// OwnerID scopes every read/write to the authenticated user.
	OwnerID uuid.UUID
	// CategoryID is nullable.
	CategoryID pgtype.UUID
	// Title is a frequently queried/displayed normal column.
	Title string
	// Metadata contains optional JSONB note-level properties.
	Metadata json.RawMessage
	// CurrentVersion increments for every note or block mutation.
	CurrentVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// DeletedAt is the note tombstone timestamp when soft deleted.
	DeletedAt pgtype.Timestamptz
}

// NoteBlock mirrors current block state from note_blocks.
type NoteBlock struct {
	// ID is the block UUID.
	ID uuid.UUID
	// NoteID is the owning note.
	NoteID uuid.UUID
	// BlockType is a string to allow future block types.
	BlockType string
	// TextContent stores the block's editable text.
	TextContent string
	// Position is the fractional-index ordering key.
	Position string
	// Properties stores type-specific JSONB properties.
	Properties json.RawMessage
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// DeletedAt is the block tombstone timestamp when soft deleted.
	DeletedAt pgtype.Timestamptz
}

// NoteChange mirrors append-only sync history from note_changes.
type NoteChange struct {
	// ID is the server-generated change UUID.
	ID uuid.UUID
	// OwnerID supports efficient per-user pull queries.
	OwnerID uuid.UUID
	// NoteID is the note affected by the operation.
	NoteID uuid.UUID
	// BlockID is nullable for note-level operations.
	BlockID pgtype.UUID
	// DeviceID identifies the device that submitted the operation.
	DeviceID uuid.UUID
	// ClientOperationID is the stable operation UUID used for idempotency.
	ClientOperationID uuid.UUID
	// EntityType is "note" or "block".
	EntityType string
	// OperationType is the concrete operation, for example update_block.
	OperationType string
	// BaseNoteVersion is the client version before the operation.
	BaseNoteVersion int64
	// ResultingNoteVersion is the server-authoritative note version after apply.
	ResultingNoteVersion int64
	// ChangeFormat describes how to interpret ChangeData.
	ChangeFormat string
	// SchemaVersion describes the version of ChangeData.
	SchemaVersion int32
	// ChangeData stores structured changed fields as JSONB.
	ChangeData json.RawMessage
	// GlobalSequence is the monotonically increasing per-database cursor.
	GlobalSequence int64
	CreatedAt      time.Time
}

// NoteSnapshot mirrors a full-note snapshot row.
type NoteSnapshot struct {
	// ID is the snapshot UUID.
	ID uuid.UUID
	// NoteID is the snapshotted note.
	NoteID uuid.UUID
	// OwnerID scopes snapshot retrieval.
	OwnerID uuid.UUID
	// Version is the note version represented by SnapshotData.
	Version int64
	// SnapshotFormat names the snapshot JSON format.
	SnapshotFormat string
	// SchemaVersion is used for future snapshot migrations.
	SchemaVersion int32
	// SnapshotData stores the full note document.
	SnapshotData json.RawMessage
	// Checksum is a SHA-256 digest of SnapshotData.
	Checksum  string
	CreatedAt time.Time
}

// OutboxJob mirrors a background job row.
type OutboxJob struct {
	// ID is the identity primary key used by workers.
	ID int64
	// JobType controls worker dispatch.
	JobType string
	// Payload stores job-specific JSON.
	Payload json.RawMessage
	// AvailableAt delays retry or scheduled jobs until a future timestamp.
	AvailableAt time.Time
	// Attempts counts claims, including failed attempts.
	Attempts int32
	// LockedAt is valid while a worker owns the job.
	LockedAt pgtype.Timestamptz
	// LockedBy stores the worker ID that claimed the job.
	LockedBy pgtype.Text
	// CompletedAt is valid when the job has finished successfully.
	CompletedAt pgtype.Timestamptz
	// LastError stores the last retryable failure message.
	LastError pgtype.Text
	CreatedAt time.Time
}

// CreateDeviceParams is the store input for inserting a sync device.
type CreateDeviceParams struct {
	// ID is supplied by the client or generated by the service.
	ID uuid.UUID
	// OwnerID is the authenticated user.
	OwnerID uuid.UUID
	// DeviceName, Platform, and AppVersion are optional metadata.
	DeviceName *string
	Platform   *string
	AppVersion *string
	// ProtocolVersion is currently expected to be 1.
	ProtocolVersion int32
}

// InsertNoteChangeParams is the store input for appending a sync change record.
type InsertNoteChangeParams struct {
	ID                uuid.UUID
	OwnerID           uuid.UUID
	NoteID            uuid.UUID
	BlockID           *uuid.UUID
	DeviceID          uuid.UUID
	ClientOperationID uuid.UUID
	EntityType        string
	OperationType     string
	// Base/resulting versions are copied into history for conflict diagnostics
	// and deterministic replay.
	BaseNoteVersion      int64
	ResultingNoteVersion int64
	ChangeFormat         string
	SchemaVersion        int32
	ChangeData           json.RawMessage
}

// NoteDocument groups one note with all of its blocks for read responses and
// snapshot creation.
type NoteDocument struct {
	// Note is the parent note row.
	Note Note
	// Blocks are returned in position order.
	Blocks []NoteBlock
}

// NullableUUID converts a pointer UUID into a nil-or-value SQL argument.
func NullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

// UUIDPtr converts a nullable pgtype.UUID into a JSON-friendly pointer.
func UUIDPtr(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

// TextPtr converts nullable PostgreSQL text into an optional JSON string.
func TextPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// TimePtr converts nullable PostgreSQL timestamptz into an optional JSON time.
func TimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

// NullableText converts a string pointer into a nil-or-value SQL argument.
func NullableText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// NormalizeJSON ensures optional JSONB documents are emitted as {} rather than
// null or an empty byte slice.
func NormalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

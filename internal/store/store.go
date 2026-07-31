package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrNotFound = errors.New("not found")

type SyncDevice struct {
	ID               uuid.UUID
	OwnerID          uuid.UUID
	DeviceName       pgtype.Text
	Platform         pgtype.Text
	AppVersion       pgtype.Text
	ProtocolVersion  int32
	LastGlobalCursor int64
	LastSeenAt       time.Time
	CreatedAt        time.Time
	RevokedAt        pgtype.Timestamptz
}

type Note struct {
	ID             uuid.UUID
	OwnerID        uuid.UUID
	CategoryID     pgtype.UUID
	Title          string
	Metadata       json.RawMessage
	CurrentVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      pgtype.Timestamptz
}

type NoteBlock struct {
	ID             uuid.UUID
	NoteID         uuid.UUID
	BlockType      string
	TextContent    string
	Position       string
	Properties     json.RawMessage
	CurrentVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      pgtype.Timestamptz
}

type NoteChange struct {
	ID                    uuid.UUID
	OwnerID               uuid.UUID
	NoteID                uuid.UUID
	BlockID               pgtype.UUID
	DeviceID              uuid.UUID
	ClientOperationID     uuid.UUID
	EntityType            string
	OperationType         string
	BaseNoteVersion       int64
	ResultingNoteVersion  int64
	BaseBlockVersion      pgtype.Int8
	ResultingBlockVersion pgtype.Int8
	ChangeFormat          string
	SchemaVersion         int32
	ChangeData            json.RawMessage
	GlobalSequence        int64
	CreatedAt             time.Time
}

type NoteSnapshot struct {
	ID             uuid.UUID
	NoteID         uuid.UUID
	OwnerID        uuid.UUID
	Version        int64
	SnapshotFormat string
	SchemaVersion  int32
	SnapshotData   json.RawMessage
	Checksum       string
	CreatedAt      time.Time
}

type OutboxJob struct {
	ID          int64
	JobType     string
	Payload     json.RawMessage
	AvailableAt time.Time
	Attempts    int32
	LockedAt    pgtype.Timestamptz
	LockedBy    pgtype.Text
	CompletedAt pgtype.Timestamptz
	LastError   pgtype.Text
	CreatedAt   time.Time
}

type CreateDeviceParams struct {
	ID              uuid.UUID
	OwnerID         uuid.UUID
	DeviceName      *string
	Platform        *string
	AppVersion      *string
	ProtocolVersion int32
}

type InsertNoteChangeParams struct {
	ID                    uuid.UUID
	OwnerID               uuid.UUID
	NoteID                uuid.UUID
	BlockID               *uuid.UUID
	DeviceID              uuid.UUID
	ClientOperationID     uuid.UUID
	EntityType            string
	OperationType         string
	BaseNoteVersion       int64
	ResultingNoteVersion  int64
	BaseBlockVersion      *int64
	ResultingBlockVersion *int64
	ChangeFormat          string
	SchemaVersion         int32
	ChangeData            json.RawMessage
}

type NoteDocument struct {
	Note   Note
	Blocks []NoteBlock
}

func NullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func UUIDPtr(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func TextPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func TimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func NullableText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func NullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func Int64Ptr(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func NormalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

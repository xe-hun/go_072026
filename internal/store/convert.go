package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "notes-server/db/generated"
)

// pgUUID converts google/uuid values into pgx nullable UUID values for sqlc
// parameters.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// pgText converts optional strings into pgx nullable text.
func pgText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

// pgInt8 converts optional int64 values into pgx nullable int8.
func pgInt8(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

// pgTime converts zero time into NULL and non-zero time into timestamptz. It is
// available for future query parameters that need optional timestamps.
func pgTime(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// fromDBDevice maps a sqlc device model into the store model consumed by
// services.
func fromDBDevice(value db.SyncDevice) SyncDevice {
	return SyncDevice{
		ID:               uuid.UUID(value.ID.Bytes),
		OwnerID:          uuid.UUID(value.OwnerID.Bytes),
		DeviceName:       value.DeviceName,
		Platform:         value.Platform,
		AppVersion:       value.AppVersion,
		ProtocolVersion:  value.ProtocolVersion,
		LastGlobalCursor: value.LastGlobalCursor,
		LastSeenAt:       value.LastSeenAt.Time,
		CreatedAt:        value.CreatedAt.Time,
		RevokedAt:        value.RevokedAt,
	}
}

// fromDBNote maps a generated note row and normalizes JSONB fields.
func fromDBNote(value db.Note) Note {
	return Note{
		ID:             uuid.UUID(value.ID.Bytes),
		OwnerID:        uuid.UUID(value.OwnerID.Bytes),
		CategoryID:     value.CategoryID,
		Title:          value.Title,
		Metadata:       NormalizeJSON(json.RawMessage(value.Metadata)),
		CurrentVersion: value.CurrentVersion,
		CreatedAt:      value.CreatedAt.Time,
		UpdatedAt:      value.UpdatedAt.Time,
		DeletedAt:      value.DeletedAt,
	}
}

// fromDBBlock maps a generated block row and normalizes JSONB fields.
func fromDBBlock(value db.NoteBlock) NoteBlock {
	return NoteBlock{
		ID:             uuid.UUID(value.ID.Bytes),
		NoteID:         uuid.UUID(value.NoteID.Bytes),
		BlockType:      value.BlockType,
		TextContent:    value.TextContent,
		Position:       value.Position,
		Properties:     NormalizeJSON(json.RawMessage(value.Properties)),
		CurrentVersion: value.CurrentVersion,
		CreatedAt:      value.CreatedAt.Time,
		UpdatedAt:      value.UpdatedAt.Time,
		DeletedAt:      value.DeletedAt,
	}
}

// fromDBChange maps generated change history rows. global_sequence is generated
// by PostgreSQL and represented by sqlc as nullable pgtype.Int8.
func fromDBChange(value db.NoteChange) NoteChange {
	sequence := int64(0)
	if value.GlobalSequence.Valid {
		sequence = value.GlobalSequence.Int64
	}
	return NoteChange{
		ID:                    uuid.UUID(value.ID.Bytes),
		OwnerID:               uuid.UUID(value.OwnerID.Bytes),
		NoteID:                uuid.UUID(value.NoteID.Bytes),
		BlockID:               value.BlockID,
		DeviceID:              uuid.UUID(value.DeviceID.Bytes),
		ClientOperationID:     uuid.UUID(value.ClientOperationID.Bytes),
		EntityType:            value.EntityType,
		OperationType:         value.OperationType,
		BaseNoteVersion:       value.BaseNoteVersion,
		ResultingNoteVersion:  value.ResultingNoteVersion,
		BaseBlockVersion:      value.BaseBlockVersion,
		ResultingBlockVersion: value.ResultingBlockVersion,
		ChangeFormat:          value.ChangeFormat,
		SchemaVersion:         value.SchemaVersion,
		ChangeData:            NormalizeJSON(json.RawMessage(value.ChangeData)),
		GlobalSequence:        sequence,
		CreatedAt:             value.CreatedAt.Time,
	}
}

// fromDBSnapshot maps generated snapshot rows.
func fromDBSnapshot(value db.NoteSnapshot) NoteSnapshot {
	return NoteSnapshot{
		ID:             uuid.UUID(value.ID.Bytes),
		NoteID:         uuid.UUID(value.NoteID.Bytes),
		OwnerID:        uuid.UUID(value.OwnerID.Bytes),
		Version:        value.Version,
		SnapshotFormat: value.SnapshotFormat,
		SchemaVersion:  value.SchemaVersion,
		SnapshotData:   json.RawMessage(value.SnapshotData),
		Checksum:       value.Checksum,
		CreatedAt:      value.CreatedAt.Time,
	}
}

// fromDBOutboxJob maps generated outbox job rows.
func fromDBOutboxJob(value db.OutboxJob) OutboxJob {
	return OutboxJob{
		ID:          value.ID,
		JobType:     value.JobType,
		Payload:     json.RawMessage(value.Payload),
		AvailableAt: value.AvailableAt.Time,
		Attempts:    value.Attempts,
		LockedAt:    value.LockedAt,
		LockedBy:    value.LockedBy,
		CompletedAt: value.CompletedAt,
		LastError:   value.LastError,
		CreatedAt:   value.CreatedAt.Time,
	}
}

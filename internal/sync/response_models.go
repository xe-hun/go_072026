package syncapi

import (
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	constants "notes-server/internal"
	"notes-server/internal/store"
	"time"
)

// FromEntity maps a persistence change into the pulled-change response.
func (c *PulledChange) FromEntity(entity store.NoteChange) error {
	if entity.ID == uuid.Nil || entity.ClientOperationID == uuid.Nil || !constants.ValidDeviceID(entity.DeviceID) {
		return errors.New("change entity must contain valid identifiers")
	}
	*c = PulledChange{
		NoteID:               entity.NoteID,
		DeviceID:             entity.DeviceID,
		OperationType:        entity.OperationType,
		BaseNoteVersion:      entity.BaseNoteVersion,
		ResultingNoteVersion: entity.ResultingNoteVersion,
		ChangeFormat:         entity.ChangeFormat,
		SchemaVersion:        entity.SchemaVersion,
		ChangeData:           store.NormalizeJSON(entity.ChangeData),
		GlobalSequence:       entity.GlobalSequence,
		CreatedAt:            entity.CreatedAt,
	}
	return nil
}

// FromEntity maps a persistence note document into the rejection snapshot.
func (s *NoteSnapshot) FromEntity(entity store.NoteDocument) error {
	if entity.Note.ID == uuid.Nil || entity.Note.OwnerID == uuid.Nil {
		return errors.New("note entity must contain valid identifiers")
	}
	blocks := make([]BlockSnapshot, 0, len(entity.Blocks))
	for _, blockEntity := range entity.Blocks {
		var block BlockSnapshot
		if err := block.FromEntity(blockEntity); err != nil {
			return err
		}
		blocks = append(blocks, block)
	}
	*s = NoteSnapshot{
		NoteID:         entity.Note.ID,
		OwnerID:        entity.Note.OwnerID,
		Title:          entity.Note.Title,
		NoteProperties: store.NormalizeJSON(entity.Note.NoteProperties),
		CurrentVersion: entity.Note.CurrentVersion,
		CreatedAt:      entity.Note.CreatedAt,
		UpdatedAt:      entity.Note.UpdatedAt,
		DeletedAt:      store.TimePtr(entity.Note.DeletedAt),
		Blocks:         blocks,
	}
	return nil
}

// FromEntity maps a persistence change into a pulled-change response.
func (s *BlockSnapshot) FromEntity(entity store.NoteBlock) error {
	if entity.ID == uuid.Nil || entity.NoteID == uuid.Nil {
		return errors.New("block entity must contain valid identifiers")
	}
	*s = BlockSnapshot{
		BlockId:         entity.ID,
		BlockType:       entity.BlockType,
		TextContent:     entity.TextContent,
		Position:        entity.Position,
		BlockProperties: store.NormalizeJSON(entity.BlockProperties),
		CreatedAt:       entity.CreatedAt,
		UpdatedAt:       entity.UpdatedAt,
		DeletedAt:       store.TimePtr(entity.DeletedAt),
	}
	return nil
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nullableTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	constants "notes-server/internal"
	"notes-server/internal/store"
	. "notes-server/internal/sync/operations"
	"time"
)

// NoteModel is the create_note payload and complete note entity model.
type NoteModel struct {
	ID             uuid.UUID
	OwnerID        uuid.UUID
	Title          string
	NoteProperties json.RawMessage
	CurrentVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	Blocks         []BlockModel
}

// FromRequest parses and validates create_note changeData.
func (m *NoteModel) FromRequest(op ClientOperation, ownerID uuid.UUID) error {
	if op.NoteID == uuid.Nil || ownerID == uuid.Nil {
		return errors.New("note identifiers are required")
	}
	fields, err := DecodeChangeObject(op.ChangeData)
	if err != nil {
		return err
	}
	title, ok, err := GetStringField(fields, constants.RequestKeyTitle)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyTitle)
	}
	noteProperties, ok, err := GetObjectField(fields, constants.RequestKeyNoteProperties)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyNoteProperties)
	}
	blockValues, ok, err := GetArrayField(fields, constants.RequestKeyBlocks)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyBlocks)
	}
	blocks := make([]BlockModel, 0, len(blockValues))
	blockIDs := make(map[uuid.UUID]struct{}, len(blockValues))
	positions := make(map[string]struct{}, len(blockValues))
	for i, rawBlock := range blockValues {
		var block BlockModel
		if err := block.FromRequest(rawBlock); err != nil {
			return fmt.Errorf("%s[%d]: %w", constants.RequestKeyBlocks, i, err)
		}
		position := block.Position
		if _, exists := blockIDs[block.BlockId]; exists {
			return fmt.Errorf("%s contains duplicate ids", constants.RequestKeyBlocks)
		}
		if _, exists := positions[position]; exists {
			return fmt.Errorf("%s contains duplicate positions", constants.RequestKeyBlocks)
		}
		blockIDs[block.BlockId] = struct{}{}
		positions[position] = struct{}{}
		blocks = append(blocks, block)
	}
	m.ID = op.NoteID
	m.OwnerID = ownerID
	m.Title = title
	m.NoteProperties = noteProperties
	m.CurrentVersion = 0
	m.Blocks = blocks
	return nil
}

// FromEntity maps a complete persistence note document into NoteModel.
func (m *NoteModel) FromEntity(entity store.NoteDocument) error {
	if entity.Note.ID == uuid.Nil || entity.Note.OwnerID == uuid.Nil {
		return errors.New("note entity must contain valid identifiers")
	}
	m.ID = entity.Note.ID
	m.OwnerID = entity.Note.OwnerID
	m.Title = entity.Note.Title
	m.NoteProperties = store.NormalizeJSON(entity.Note.NoteProperties)
	m.CurrentVersion = entity.Note.CurrentVersion
	m.CreatedAt = entity.Note.CreatedAt
	m.UpdatedAt = entity.Note.UpdatedAt
	m.DeletedAt = store.TimePtr(entity.Note.DeletedAt)
	m.Blocks = make([]BlockModel, 0, len(entity.Blocks))
	for _, blockEntity := range entity.Blocks {
		if blockEntity.NoteID != entity.Note.ID {
			return errors.New("block entity does not belong to note entity")
		}
		var block BlockModel
		if err := block.FromEntity(blockEntity); err != nil {
			return err
		}
		m.Blocks = append(m.Blocks, block)
	}
	return nil
}

// Entity converts NoteModel into a persistence note document.
func (m NoteModel) Entity() (store.NoteDocument, error) {
	if m.ID == uuid.Nil || m.OwnerID == uuid.Nil {
		return store.NoteDocument{}, errors.New("note identifiers are required")
	}
	document := store.NoteDocument{
		Note: store.Note{
			ID:             m.ID,
			OwnerID:        m.OwnerID,
			Title:          m.Title,
			NoteProperties: store.NormalizeJSON(m.NoteProperties),
			CurrentVersion: m.CurrentVersion,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
			DeletedAt:      nullableTime(m.DeletedAt),
		},
		Blocks: make([]store.NoteBlock, 0, len(m.Blocks)),
	}
	for _, blockModel := range m.Blocks {
		block, err := blockModel.Entity(m.ID)
		if err != nil {
			return store.NoteDocument{}, err
		}
		document.Blocks = append(document.Blocks, block)
	}
	return document, nil
}

package notes

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"notes-server/internal/store"
)

// Block is the note block model used between persistence entities and API
// responses.
type Block struct {
	ID              uuid.UUID
	NoteID          uuid.UUID
	BlockType       string
	TextContent     string
	Position        string
	BlockProperties json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// FromEntity maps a persistence block entity into the block model.
func (m *Block) FromEntity(entity store.NoteBlock) error {
	if entity.ID == uuid.Nil || entity.NoteID == uuid.Nil {
		return errors.New("block entity must contain valid identifiers")
	}
	m.ID = entity.ID
	m.NoteID = entity.NoteID
	m.BlockType = entity.BlockType
	m.TextContent = entity.TextContent
	m.Position = entity.Position
	m.BlockProperties = store.NormalizeJSON(entity.BlockProperties)
	m.CreatedAt = entity.CreatedAt
	m.UpdatedAt = entity.UpdatedAt
	m.DeletedAt = store.TimePtr(entity.DeletedAt)
	return nil
}

// Entity converts the block model into a persistence block entity.
func (m Block) Entity() (store.NoteBlock, error) {
	if m.ID == uuid.Nil || m.NoteID == uuid.Nil {
		return store.NoteBlock{}, errors.New("block identifiers are required")
	}
	return store.NoteBlock{
		ID:              m.ID,
		NoteID:          m.NoteID,
		BlockType:       m.BlockType,
		TextContent:     m.TextContent,
		Position:        m.Position,
		BlockProperties: store.NormalizeJSON(m.BlockProperties),
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		DeletedAt:       nullableTime(m.DeletedAt),
	}, nil
}

// Response converts the block model into its public API representation.
func (m Block) Response() BlockResponse {
	return BlockResponse{
		ID:              m.ID,
		NoteID:          m.NoteID,
		BlockType:       m.BlockType,
		TextContent:     m.TextContent,
		Position:        m.Position,
		BlockProperties: store.NormalizeJSON(m.BlockProperties),
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		DeletedAt:       m.DeletedAt,
	}
}

// FromEntity maps a persistence block entity directly into the API response.
func (r *BlockResponse) FromEntity(entity store.NoteBlock) error {
	var model Block
	if err := model.FromEntity(entity); err != nil {
		return err
	}
	*r = model.Response()
	return nil
}

// Note is the complete note model used between persistence entities and API
// responses.
type Note struct {
	ID             uuid.UUID
	OwnerID        uuid.UUID
	Title          string
	NoteProperties json.RawMessage
	CurrentVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	Blocks         []Block
}

// FromEntity maps a complete persistence note document into the note model.
func (m *Note) FromEntity(entity store.NoteDocument) error {
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
	m.Blocks = make([]Block, 0, len(entity.Blocks))
	for _, blockEntity := range entity.Blocks {
		if blockEntity.NoteID != entity.Note.ID {
			return errors.New("block entity does not belong to note entity")
		}
		var block Block
		if err := block.FromEntity(blockEntity); err != nil {
			return err
		}
		m.Blocks = append(m.Blocks, block)
	}
	return nil
}

// Entity converts the note model into a complete persistence note document.
func (m Note) Entity() (store.NoteDocument, error) {
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
		block, err := blockModel.Entity()
		if err != nil {
			return store.NoteDocument{}, err
		}
		if block.NoteID != m.ID {
			return store.NoteDocument{}, errors.New("block model does not belong to note model")
		}
		document.Blocks = append(document.Blocks, block)
	}
	return document, nil
}

// Response converts the note model into its public API representation.
func (m Note) Response() NoteResponse {
	blocks := make([]BlockResponse, 0, len(m.Blocks))
	for _, block := range m.Blocks {
		blocks = append(blocks, block.Response())
	}
	return NoteResponse{
		ID:             m.ID,
		OwnerID:        m.OwnerID,
		Title:          m.Title,
		NoteProperties: store.NormalizeJSON(m.NoteProperties),
		CurrentVersion: m.CurrentVersion,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		DeletedAt:      m.DeletedAt,
		Blocks:         blocks,
	}
}

// FromEntity maps a persistence note document directly into the API response.
func (r *NoteResponse) FromEntity(entity store.NoteDocument) error {
	var model Note
	if err := model.FromEntity(entity); err != nil {
		return err
	}
	*r = model.Response()
	return nil
}

// FromEntity maps a persistence snapshot entity into the API response.
func (r *SnapshotResponse) FromEntity(entity store.NoteSnapshot) error {
	if entity.ID == uuid.Nil || entity.NoteID == uuid.Nil || entity.OwnerID == uuid.Nil {
		return errors.New("snapshot entity must contain valid identifiers")
	}
	*r = SnapshotResponse{
		ID:             entity.ID,
		NoteID:         entity.NoteID,
		OwnerID:        entity.OwnerID,
		Version:        entity.Version,
		SnapshotFormat: entity.SnapshotFormat,
		SchemaVersion:  entity.SchemaVersion,
		SnapshotData:   store.NormalizeJSON(entity.SnapshotData),
		Checksum:       entity.Checksum,
		CreatedAt:      entity.CreatedAt,
	}
	return nil
}

func nullableTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

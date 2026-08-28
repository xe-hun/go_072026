package syncapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	constants "notes-server/internal"
	"notes-server/internal/store"
	"strconv"
	"strings"
)

// create_note.
type BlockModel struct {
	BlockId         uuid.UUID
	BlockType       string
	Position        string
	TextContent     string
	BlockProperties json.RawMessage
}

// FromCreateOperation parses the create_block changeData wrapper.
func (m *BlockModel) FromCreateOperation(raw json.RawMessage) error {
	fields, err := decodeChangeObject(raw)
	if err != nil {
		return err
	}
	block, ok, err := getObjectField(fields, constants.RequestKeyBlock)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s requires %s", OperationCreateBlock, constants.RequestKeyBlock)
	}
	return m.FromRequest(block)
}

// FromRequest parses and validates a direct block payload.
func (m *BlockModel) FromRequest(raw json.RawMessage) error {
	blockSnapShotFields, err := decodeObjectFields(raw, constants.RequestKeyBlock)
	if err != nil {
		return err
	}
	blockId, ok, err := getUUIDField(blockSnapShotFields, constants.RequestKeyBlockId)
	if err != nil {
		return err
	}
	if !ok || blockId == uuid.Nil {
		return fmt.Errorf("%s is required", constants.RequestKeyBlockId)
	}
	blockType, ok, err := getStringField(blockSnapShotFields, constants.RequestKeyBlockType)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyBlockType)
	}
	if err := validateBlockType(blockType); err != nil {
		return err
	}
	position, ok, err := getFloatField(blockSnapShotFields, constants.RequestKeyPosition)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyPosition)
	}
	if position < 0 {
		return fmt.Errorf("%s must be greater than or equal to zero", constants.RequestKeyPosition)
	}
	textContent, ok, err := getStringField(blockSnapShotFields, constants.RequestKeyTextContent)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyTextContent)
	}
	blockProperties, ok, err := getObjectField(blockSnapShotFields, constants.RequestKeyBlockProperties)
	if err != nil {
		return err
	}
	if !ok {
		blockProperties = json.RawMessage(`{}`)
	}
	m.BlockId = blockId
	m.BlockType = blockType
	m.Position = strconv.FormatFloat(position, 'f', -1, 64)
	m.TextContent = textContent
	m.BlockProperties = blockProperties
	return nil
}

// FromEntity maps a persistence block into the block payload model.
func (m *BlockModel) FromEntity(entity store.NoteBlock) error {
	if entity.ID == uuid.Nil || entity.NoteID == uuid.Nil {
		return errors.New("block entity must contain valid identifiers")
	}
	m.BlockId = entity.ID
	m.BlockType = entity.BlockType
	m.Position = entity.Position
	m.TextContent = entity.TextContent
	m.BlockProperties = store.NormalizeJSON(entity.BlockProperties)
	return nil
}

// Entity converts the block payload model into a persistence block entity.
func (m BlockModel) Entity(noteID uuid.UUID) (store.NoteBlock, error) {
	if noteID == uuid.Nil || m.BlockId == uuid.Nil {
		return store.NoteBlock{}, errors.New("block identifiers are required")
	}
	if err := validateBlockType(m.BlockType); err != nil {
		return store.NoteBlock{}, err
	}
	if strings.TrimSpace(m.Position) == "" {
		return store.NoteBlock{}, fmt.Errorf("%s is required", constants.RequestKeyPosition)
	}
	return store.NoteBlock{
		ID:              m.BlockId,
		NoteID:          noteID,
		BlockType:       m.BlockType,
		TextContent:     m.TextContent,
		Position:        m.Position,
		BlockProperties: store.NormalizeJSON(m.BlockProperties),
	}, nil
}

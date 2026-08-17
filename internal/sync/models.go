package syncapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Validate checks request-level invariants that do not depend on database
// state.
func (r Request) Validate(maxOperations int) error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == uuid.Nil {
		return httpapi.InvalidRequest("deviceId is required.")
	}
	if len(r.Operations) > maxOperations {
		return httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The sync request contains too many operations.")
	}
	return nil
}

// PullLimit validates and normalizes the request pull limit.
func (r Request) PullLimit(defaultLimit, maxLimit int32) (int32, error) {
	limit := r.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 {
		return 0, httpapi.InvalidRequest("limit must be greater than zero.")
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit, nil
}

// Validate checks operation-level invariants before database state is read.
func (op ClientOperation) Validate() error {
	if op.OperationID == uuid.Nil {
		return errors.New("operationId is required")
	}
	if op.Sequence < 0 {
		return errors.New("sequence must be greater than or equal to zero")
	}
	switch op.OperationType {
	case OperationCreateCategory, OperationDeleteCategory, OperationModifyCategory:
		if op.NoteID != uuid.Nil {
			return errors.New("noteId is not allowed for category operations")
		}
	case OperationCreateNote, OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		if op.NoteID == uuid.Nil {
			return errors.New("noteId is required")
		}
	case OperationCreateBlock, OperationDeleteBlock, OperationModifyBlock:
		if op.NoteID == uuid.Nil {
			return errors.New("noteId is required")
		}
	default:
		return errors.New("operationType is unsupported")
	}
	if normalizeChangeFormat(op.ChangeFormat) != ChangeFormatStructuredV1 {
		return errors.New("changeFormat is unsupported")
	}
	return nil
}

// Operation is the validated client operation model with decoded changeData.
type Operation struct {
	ClientOperation ClientOperation
	Fields          map[string]json.RawMessage
}

// NoteMutation is the validated payload model for note updates.
type NoteMutation struct {
	NoteProperties map[string]json.RawMessage
	HasProperties  bool
	TextChange     TextChange
	HasTextChange  bool
}

// FromRequest parses and validates a note mutation payload.
func (m *NoteMutation) FromRequest(raw json.RawMessage, operationType string) error {
	fields, err := decodeObjectFields(raw, "changeData")
	if err != nil {
		return err
	}
	switch operationType {
	case OperationModifyNoteProperty:
		propertiesRaw, ok, err := getObjectField(fields, "noteProperties")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("modify_note_property must include at least one property")
		}
		properties, err := decodeObjectFields(propertiesRaw, "noteProperties")
		if err != nil {
			return err
		}
		if len(properties) == 0 {
			return errors.New("modify_note_property must include at least one property")
		}
		m.NoteProperties = properties
		m.HasProperties = true
	case OperationModifyNoteTitle:
		textDelta, ok, err := getObjectField(fields, "textDelta")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("modify_note_title requires textDelta")
		}
		m.TextChange, err = decodeTextChange(textDelta, "textDelta")
		if err != nil {
			return err
		}
		m.HasTextChange = true
	case OperationDeleteNote:
		return nil
	default:
		return errors.New("unsupported note mutation")
	}
	return nil
}

// BlockMutation is the validated payload model for block updates and deletes.
type BlockMutation struct {
	ID                uuid.UUID
	ChangedProperties map[string]json.RawMessage
	HasProperties     bool
	TextChange        TextChange
	HasTextChange     bool
}

// FromRequest parses and validates a block mutation payload.
func (m *BlockMutation) FromRequest(raw json.RawMessage, operationType string) error {
	fields, err := decodeObjectFields(raw, "changeData")
	if err != nil {
		return err
	}
	id, ok, err := getUUIDField(fields, "id")
	if err != nil {
		return err
	}
	if !ok || id == uuid.Nil {
		return errors.New("id is required")
	}
	m.ID = id
	if operationType == OperationDeleteBlock {
		return nil
	}
	if operationType != OperationModifyBlock {
		return errors.New("unsupported block mutation")
	}
	propertiesRaw, hasProperties := fields["blockProperties"]
	if hasProperties && !isJSONNull(propertiesRaw) {
		propertiesRaw, _, err = getObjectField(fields, "blockProperties")
		if err != nil {
			return err
		}
		m.ChangedProperties, err = decodeObjectFields(propertiesRaw, "blockProperties")
		if err != nil {
			return err
		}
	}
	textDelta, hasTextDelta, err := getObjectField(fields, "textDelta")
	if err != nil {
		return err
	}
	if hasTextDelta {
		m.TextChange, err = decodeTextChange(textDelta, "textDelta")
		if err != nil {
			return err
		}
		m.HasTextChange = true
	}
	if !hasProperties && !m.HasTextChange {
		return errors.New("modify_block must include blockProperties or textDelta")
	}
	m.HasProperties = hasProperties
	return nil
}

// Category is the validated category payload model.
type Category struct {
	ID   uuid.UUID
	Name string
}

// FromRequest parses and validates a category payload.
func (m *Category) FromRequest(raw json.RawMessage, requireName bool) error {
	fields, err := decodeObjectFields(raw, "changeData")
	if err != nil {
		return err
	}
	id, ok, err := getUUIDField(fields, "id")
	if err != nil {
		return err
	}
	if !ok || id == uuid.Nil {
		return errors.New("id is required")
	}
	m.ID = id
	if !requireName {
		return nil
	}
	name, ok, err := getStringField(fields, "name")
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	m.Name = name
	return nil
}

// FromRequest validates an operation envelope and decodes its direct payload.
func (m *Operation) FromRequest(request ClientOperation) error {
	if err := request.Validate(); err != nil {
		return err
	}
	fields, err := decodeChangeObject(request.ChangeData)
	if err != nil {
		return errors.New("changeData is invalid.")
	}
	m.ClientOperation = request
	m.Fields = fields
	return nil
}

// Entity converts a client operation into its append-only change entity after
// the service has supplied authenticated and versioning context.
func (op ClientOperation) Entity(ownerID, deviceID uuid.UUID, blockID *uuid.UUID, baseVersion, resultingVersion int64) (store.InsertNoteChangeParams, error) {
	if err := op.Validate(); err != nil {
		return store.InsertNoteChangeParams{}, err
	}
	if ownerID == uuid.Nil || deviceID == uuid.Nil {
		return store.InsertNoteChangeParams{}, errors.New("owner and device identifiers are required")
	}
	if (op.OperationType == OperationCreateBlock || op.OperationType == OperationDeleteBlock || op.OperationType == OperationModifyBlock) && (blockID == nil || *blockID == uuid.Nil) {
		return store.InsertNoteChangeParams{}, errors.New("block identifier is required")
	}
	return store.InsertNoteChangeParams{
		ID:                   uuid.New(),
		OwnerID:              ownerID,
		NoteID:               nullableUUID(op.NoteID),
		BlockID:              blockID,
		DeviceID:             deviceID,
		ClientOperationID:    op.OperationID,
		OperationType:        op.OperationType,
		BaseNoteVersion:      baseVersion,
		ResultingNoteVersion: resultingVersion,
		ChangeFormat:         normalizeChangeFormat(op.ChangeFormat),
		SchemaVersion:        1,
		ChangeData:           op.ChangeData,
	}, nil
}

// BlockModel is the typed block payload model used by block operations and
// create_note.
type BlockModel struct {
	ID              uuid.UUID
	BlockType       string
	Position        string
	TextContent     string
	BlockProperties json.RawMessage
}

// FromRequest parses and validates a direct block payload.
func (m *BlockModel) FromRequest(raw json.RawMessage) error {
	fields, err := decodeObjectFields(raw, "block")
	if err != nil {
		return err
	}
	id, ok, err := getUUIDField(fields, "id")
	if err != nil {
		return err
	}
	if !ok || id == uuid.Nil {
		return errors.New("id is required")
	}
	blockType, ok, err := getStringField(fields, "blockType")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("blockType is required")
	}
	if err := validateBlockType(blockType); err != nil {
		return err
	}
	position, ok, err := getFloatField(fields, "position")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("position is required")
	}
	if position < 0 {
		return errors.New("position must be greater than or equal to zero")
	}
	textContent, ok, err := getStringField(fields, "textContent")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("textContent is required")
	}
	blockProperties, ok, err := getObjectField(fields, "blockProperties")
	if err != nil {
		return err
	}
	if !ok {
		blockProperties = json.RawMessage(`{}`)
	}
	m.ID = id
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
	m.ID = entity.ID
	m.BlockType = entity.BlockType
	m.Position = entity.Position
	m.TextContent = entity.TextContent
	m.BlockProperties = store.NormalizeJSON(entity.BlockProperties)
	return nil
}

// Entity converts the block payload model into a persistence block entity.
func (m BlockModel) Entity(noteID uuid.UUID) (store.NoteBlock, error) {
	if noteID == uuid.Nil || m.ID == uuid.Nil {
		return store.NoteBlock{}, errors.New("block identifiers are required")
	}
	if err := validateBlockType(m.BlockType); err != nil {
		return store.NoteBlock{}, err
	}
	if strings.TrimSpace(m.Position) == "" {
		return store.NoteBlock{}, errors.New("position is required")
	}
	return store.NoteBlock{
		ID:              m.ID,
		NoteID:          noteID,
		BlockType:       m.BlockType,
		TextContent:     m.TextContent,
		Position:        m.Position,
		BlockProperties: store.NormalizeJSON(m.BlockProperties),
	}, nil
}

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
	fields, err := decodeChangeObject(op.ChangeData)
	if err != nil {
		return err
	}
	title, ok, err := getStringField(fields, "title")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("title is required")
	}
	noteProperties, ok, err := getObjectField(fields, "noteProperties")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("noteProperties is required")
	}
	blockValues, ok, err := getArrayField(fields, "blocks")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("blocks is required")
	}
	blocks := make([]BlockModel, 0, len(blockValues))
	blockIDs := make(map[uuid.UUID]struct{}, len(blockValues))
	positions := make(map[string]struct{}, len(blockValues))
	for i, rawBlock := range blockValues {
		var block BlockModel
		if err := block.FromRequest(rawBlock); err != nil {
			return fmt.Errorf("blocks[%d]: %w", i, err)
		}
		position := block.Position
		if _, exists := blockIDs[block.ID]; exists {
			return errors.New("blocks contains duplicate ids")
		}
		if _, exists := positions[position]; exists {
			return errors.New("blocks contains duplicate positions")
		}
		blockIDs[block.ID] = struct{}{}
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

// FromEntity maps a persistence change into the pulled-change response.
func (c *PulledChange) FromEntity(entity store.NoteChange) error {
	if entity.ID == uuid.Nil || entity.ClientOperationID == uuid.Nil || entity.DeviceID == uuid.Nil {
		return errors.New("change entity must contain valid identifiers")
	}
	*c = PulledChange{
		ID:                   entity.ID,
		OperationID:          entity.ClientOperationID,
		NoteID:               entity.NoteID,
		BlockID:              store.UUIDPtr(entity.BlockID),
		DeviceID:             entity.DeviceID,
		OperationType:        entity.OperationType,
		BaseNoteVersion:      entity.BaseNoteVersion,
		ResultingNoteVersion: entity.ResultingNoteVersion,
		ChangeFormat:         entity.ChangeFormat,
		SchemaVersion:        entity.SchemaVersion,
		ChangeData:           store.NormalizeJSON(entity.ChangeData),
		Sequence:             entity.GlobalSequence,
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
		ID:             entity.Note.ID,
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
		ID:              entity.ID,
		NoteID:          entity.NoteID,
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

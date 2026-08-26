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

	constants "notes-server/internal"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Validate checks request-level invariants that do not depend on database
// state.
func (r Request) Validate(maxOperations int) error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	if len(r.Operations) > maxOperations {
		return httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The sync request contains too many operations.")
	}
	return nil
}

// Validate checks pull request query parameters.
func (r PullRequest) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	if r.Cursor < 0 {
		return httpapi.InvalidRequest(constants.RequestKeyCursor + " must be greater than or equal to zero.")
	}
	return nil
}

// Validate checks update availability query parameters.
func (r UpdatesAvailableRequest) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	return nil
}

// Validate checks operation-level invariants before database state is read.
func (op ClientOperation) Validate() error {
	if op.OperationID == uuid.Nil {
		return fmt.Errorf("%s is required", constants.RequestKeyOperationID)
	}
	if op.Sequence < 0 {
		return fmt.Errorf("%s must be greater than or equal to zero", constants.RequestKeySequence)
	}
	switch op.OperationType {
	case OperationCreateCategory, OperationDeleteCategory, OperationModifyCategory:
		if op.NoteID != uuid.Nil {
			return fmt.Errorf("%s is not allowed for category operations", constants.RequestKeyNoteID)
		}
	case OperationCreateNote, OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		if op.NoteID == uuid.Nil {
			return fmt.Errorf("%s is required", constants.RequestKeyNoteID)
		}
	case OperationCreateBlock, OperationDeleteBlock, OperationModifyBlock:
		if op.NoteID == uuid.Nil {
			return fmt.Errorf("%s is required", constants.RequestKeyNoteID)
		}
	default:
		return fmt.Errorf("%s is unsupported", constants.RequestKeyOperationType)
	}
	if normalizeChangeFormat(op.ChangeFormat) != ChangeFormatStructuredV1 {
		return fmt.Errorf("%s is unsupported", constants.RequestKeyChangeFormat)
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
	TextDelta      TextDelta
	HasTextDelta   bool
}

// FromRequest parses and validates a note mutation payload.
func (m *NoteMutation) FromRequest(raw json.RawMessage, operationType string) error {
	fields, err := decodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	switch operationType {
	case OperationModifyNoteProperty:
		propertiesRaw, ok, err := getObjectField(fields, constants.RequestKeyNoteProperties)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s must include at least one property", OperationModifyNoteProperty)
		}
		properties, err := decodeObjectFields(propertiesRaw, constants.RequestKeyNoteProperties)
		if err != nil {
			return err
		}
		if len(properties) == 0 {
			return fmt.Errorf("%s must include at least one property", OperationModifyNoteProperty)
		}
		m.NoteProperties = properties
		m.HasProperties = true
	case OperationModifyNoteTitle:
		textDelta, ok, err := getObjectField(fields, constants.RequestKeyTextDelta)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s requires %s", OperationModifyNoteTitle, constants.RequestKeyTextDelta)
		}
		m.TextDelta, err = decodeTextDelta(textDelta, constants.RequestKeyTextDelta)
		if err != nil {
			return err
		}
		m.HasTextDelta = true
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
	TextDelta         TextDelta
	HasTextDelta      bool
}

// FromRequest parses and validates a block mutation payload.
func (m *BlockMutation) FromRequest(raw json.RawMessage, operationType string) error {
	changeDataFields, err := decodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	id, ok, err := getUUIDField(changeDataFields, constants.RequestKeyBlockId)
	if err != nil {
		return err
	}
	if !ok || id == uuid.Nil {
		return fmt.Errorf("%s is required", constants.RequestKeyBlockId)
	}
	m.ID = id
	if operationType == OperationDeleteBlock {
		return nil
	}
	if operationType != OperationModifyBlock {
		return errors.New("unsupported block mutation")
	}
	propertiesRaw, hasProperties := changeDataFields[constants.RequestKeyChangedProperties]
	if hasProperties && !isJSONNull(propertiesRaw) {
		propertiesRaw, _, err = getObjectField(changeDataFields, constants.RequestKeyChangedProperties)
		if err != nil {
			return err
		}
		m.ChangedProperties, err = decodeObjectFields(propertiesRaw, constants.RequestKeyChangedProperties)
		if err != nil {
			return err
		}
	}
	textDelta, hasTextDelta, err := getObjectField(changeDataFields, constants.RequestKeyTextDelta)
	if err != nil {
		return err
	}
	if hasTextDelta {
		m.TextDelta, err = decodeTextDelta(textDelta, constants.RequestKeyTextDelta)
		if err != nil {
			return err
		}
		m.HasTextDelta = true
	}
	if !hasProperties && !m.HasTextDelta {
		return fmt.Errorf("%s must include %s or %s", OperationModifyBlock, constants.RequestKeyChangedProperties, constants.RequestKeyTextDelta)
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
	fields, err := decodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	id, ok, err := getUUIDField(fields, constants.RequestKeyCategoryId)
	if err != nil {
		return err
	}
	if !ok || id == uuid.Nil {
		return fmt.Errorf("%s is required", constants.RequestKeyCategoryId)
	}
	m.ID = id
	if !requireName {
		return nil
	}
	name, ok, err := getStringField(fields, constants.RequestKeyName)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s is required", constants.RequestKeyName)
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
		return fmt.Errorf("%s is invalid.", constants.RequestKeyChangeData)
	}
	m.ClientOperation = request
	m.Fields = fields
	return nil
}

// Entity converts a client operation into its append-only change entity after
// the service has supplied authenticated and versioning context.
func (op ClientOperation) Entity(ownerID uuid.UUID, deviceID string, blockID *uuid.UUID, baseVersion, resultingVersion int64) (store.InsertNoteChangeParams, error) {
	if err := op.Validate(); err != nil {
		return store.InsertNoteChangeParams{}, err
	}
	if ownerID == uuid.Nil || !constants.ValidDeviceID(deviceID) {
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
	title, ok, err := getStringField(fields, constants.RequestKeyTitle)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyTitle)
	}
	noteProperties, ok, err := getObjectField(fields, constants.RequestKeyNoteProperties)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required", constants.RequestKeyNoteProperties)
	}
	blockValues, ok, err := getArrayField(fields, constants.RequestKeyBlocks)
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

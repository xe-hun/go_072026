package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	constants "notes-server/internal"
	"notes-server/internal/store"
	. "notes-server/internal/sync/operations"
	"strings"
)

// Operation is the validated client operation model with decoded changeData.
type Operation struct {
	ClientOperation ClientOperation
	Fields          map[string]json.RawMessage
}

// NoteMutation is the validated payload model for note updates.
type NoteMutation struct {
	NoteProperties json.RawMessage
	HasProperties  bool
	TextDelta      TextDelta
	HasTextDelta   bool
}

// FromRequest parses and validates a note mutation payload.
func (m *NoteMutation) FromRequest(raw json.RawMessage, operationType string) error {
	fields, err := DecodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	switch operationType {
	case OperationModifyNoteProperty:
		propertiesRaw, ok, err := GetObjectField(fields, constants.RequestKeyNoteProperties)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s requires %s", OperationModifyNoteProperty, constants.RequestKeyNoteProperties)
		}
		m.NoteProperties = propertiesRaw
		m.HasProperties = true
	case OperationModifyNoteTitle:
		textDelta, ok, err := GetObjectField(fields, constants.RequestKeyTextDelta)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s requires %s", OperationModifyNoteTitle, constants.RequestKeyTextDelta)
		}
		m.TextDelta, err = DecodeTextDelta(textDelta, constants.RequestKeyTextDelta)
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
	ID              uuid.UUID
	BlockProperties json.RawMessage
	HasProperties   bool
	TextDelta       TextDelta
	HasTextDelta    bool
}

// FromRequest parses and validates a block mutation payload.
func (m *BlockMutation) FromRequest(raw json.RawMessage, operationType string) error {
	changeDataFields, err := DecodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	id, ok, err := GetUUIDField(changeDataFields, constants.RequestKeyBlockId)
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
	propertiesRaw, hasProperties, err := GetObjectField(changeDataFields, constants.RequestKeyBlockProperties)
	if err != nil {
		return err
	}
	if hasProperties {
		m.BlockProperties = propertiesRaw
	}
	textDelta, hasTextDelta, err := GetObjectField(changeDataFields, constants.RequestKeyTextDelta)
	if err != nil {
		return err
	}
	if hasTextDelta {
		m.TextDelta, err = DecodeTextDelta(textDelta, constants.RequestKeyTextDelta)
		if err != nil {
			return err
		}
		m.HasTextDelta = true
	}
	if !hasProperties && !m.HasTextDelta {
		return fmt.Errorf("%s must include %s or %s", OperationModifyBlock, constants.RequestKeyBlockProperties, constants.RequestKeyTextDelta)
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
	fields, err := DecodeObjectFields(raw, constants.RequestKeyChangeData)
	if err != nil {
		return err
	}
	id, ok, err := GetUUIDField(fields, constants.RequestKeyCategoryId)
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
	name, ok, err := GetStringField(fields, constants.RequestKeyName)
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
	fields, err := DecodeChangeObject(request.ChangeData)
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
		ChangeFormat:         NormalizeChangeFormat(op.ChangeFormat),
		SchemaVersion:        1,
		ChangeData:           op.ChangeData,
	}, nil
}

// BlockModel is the typed block payload model used by block operations and

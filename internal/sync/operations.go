package syncapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// EntityNote and EntityBlock describe which table/current-state entity an
	// operation targets.
	EntityNote  = "note"
	EntityBlock = "block"

	// Note operation names.
	OperationCreateNote  = "create_note"
	OperationUpdateNote  = "update_note"
	OperationDeleteNote  = "delete_note"
	OperationRestoreNote = "restore_note"

	// Block operation names.
	OperationCreateBlock  = "create_block"
	OperationUpdateBlock  = "update_block"
	OperationMoveBlock    = "move_block"
	OperationDeleteBlock  = "delete_block"
	OperationRestoreBlock = "restore_block"

	// ChangeFormatStructuredV1 stores changed fields rather than raw character
	// diffs.
	ChangeFormatStructuredV1 = "structured-operation-v1"
)

// allowedBlockTypes is the initial block type allow-list. Unknown types are
// rejected so clients cannot write unsupported data shapes.
var allowedBlockTypes = map[string]struct{}{
	"text":          {},
	"bullet":        {},
	"todo":          {},
	"numbered_list": {},
	"attachment":    {},
}

// structuredChange is the expected shape of changeData for v1 operations.
type structuredChange struct {
	// SchemaVersion defaults to 1 when omitted.
	SchemaVersion int `json:"schemaVersion,omitempty"`
	// Fields contains operation-specific changed fields.
	Fields map[string]json.RawMessage `json:"fields"`
}

// normalizeChangeFormat treats an omitted changeFormat as the v1 default.
func normalizeChangeFormat(format string) string {
	if strings.TrimSpace(format) == "" {
		return ChangeFormatStructuredV1
	}
	return format
}

// decodeFields extracts the fields object from changeData and validates the
// embedded schemaVersion.
func decodeFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var change structuredChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return nil, err
	}
	if change.SchemaVersion != 0 && change.SchemaVersion != 1 {
		return nil, errors.New("unsupported change schema version")
	}
	if change.Fields == nil {
		change.Fields = map[string]json.RawMessage{}
	}
	return change.Fields, nil
}

// getStringField reads an optional string field. The bool reports whether the
// field was present.
func getStringField(fields map[string]json.RawMessage, key string) (string, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, fmt.Errorf("%s must be a string", key)
	}
	return value, true, nil
}

// getObjectField reads an optional JSON object field and returns the raw object
// so it can be stored in JSONB without reformatting.
func getObjectField(fields map[string]json.RawMessage, key string) (json.RawMessage, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, true, fmt.Errorf("%s must be an object", key)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, fmt.Errorf("%s must be an object", key)
	}
	return json.RawMessage(trimmed), true, nil
}

// getNullableUUIDField reads an optional UUID field that can explicitly be null.
func getNullableUUIDField(fields map[string]json.RawMessage, key string) (pgtype.UUID, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return pgtype.UUID{}, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return pgtype.UUID{}, true, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return pgtype.UUID{}, true, fmt.Errorf("%s must be a UUID or null", key)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, true, nil
}

// validateOperationShape checks operation-level invariants before any database
// rows are locked or mutated.
func validateOperationShape(op ClientOperation) error {
	if op.OperationID == uuid.Nil {
		return errors.New("operationId is required")
	}
	if op.NoteID == uuid.Nil {
		return errors.New("noteId is required")
	}
	switch op.OperationType {
	case OperationCreateNote, OperationUpdateNote, OperationDeleteNote, OperationRestoreNote:
		if op.EntityType != EntityNote {
			return errors.New("note operation must use entityType note")
		}
	case OperationCreateBlock, OperationUpdateBlock, OperationMoveBlock, OperationDeleteBlock, OperationRestoreBlock:
		if op.EntityType != EntityBlock {
			return errors.New("block operation must use entityType block")
		}
		if op.BlockID == nil || *op.BlockID == uuid.Nil {
			return errors.New("blockId is required for block operations")
		}
	default:
		return errors.New("operationType is unsupported")
	}
	if normalizeChangeFormat(op.ChangeFormat) != ChangeFormatStructuredV1 {
		return errors.New("changeFormat is unsupported")
	}
	return nil
}

// validateBlockType enforces the block type allow-list.
func validateBlockType(blockType string) error {
	if _, ok := allowedBlockTypes[blockType]; !ok {
		return fmt.Errorf("blockType %q is unsupported", blockType)
	}
	return nil
}

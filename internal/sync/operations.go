package syncapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	// Note operation names.
	OperationCreateNote         = "create_note"
	OperationDeleteNote         = "delete_note"
	OperationModifyNoteProperty = "modify_note_property"
	OperationModifyNoteTitle    = "modify_note_title"

	// Block operation names.
	OperationCreateBlock = "create_block"
	OperationDeleteBlock = "delete_block"
	OperationModifyBlock = "modify_block"

	// Category operation names.
	OperationCreateCategory = "create_category"
	OperationDeleteCategory = "delete_category"
	OperationModifyCategory = "modify_category"

	// Text operation names accepted by title/block text deltas.
	TextOperationInsert = "insert"
	TextOperationDelete = "delete"

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

// TextChange is the direct text delta shape used by modify_note_title and the
// nested modify_block.textDelta payload.
type TextChange struct {
	TextOperation string
	Index         int
	Text          string
}

// normalizeChangeFormat treats an omitted changeFormat as the v1 default.
func normalizeChangeFormat(format string) string {
	if strings.TrimSpace(format) == "" {
		return ChangeFormatStructuredV1
	}
	return format
}

// decodeChangeObject extracts the direct object payload from changeData.
func decodeChangeObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	return decodeObjectFields(raw, "changeData")
}

// decodeObjectFields validates and decodes a JSON object into raw field values.
func decodeObjectFields(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("%s must be an object", name)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	return fields, nil
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

// getIntField reads an optional integer field. The bool reports whether the
// field was present.
func getIntField(fields map[string]json.RawMessage, key string) (int, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return 0, false, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, true, fmt.Errorf("%s must be an integer", key)
	}
	return value, true, nil
}

// getFloatField reads an optional JSON number.
func getFloatField(fields map[string]json.RawMessage, key string) (float64, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return 0, false, nil
	}
	if isJSONNull(raw) {
		return 0, true, fmt.Errorf("%s must be a number", key)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, true, fmt.Errorf("%s must be a number", key)
	}
	return value, true, nil
}

// getArrayField reads an optional JSON array as raw element values.
func getArrayField(fields map[string]json.RawMessage, key string) ([]json.RawMessage, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	if isJSONNull(raw) {
		return nil, true, fmt.Errorf("%s must be an array", key)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, true, fmt.Errorf("%s must be an array", key)
	}
	return values, true, nil
}

// getObjectField reads an optional JSON object field and returns the raw object
// so it can be stored in JSONB without reformatting.
func getObjectField(fields map[string]json.RawMessage, key string) (json.RawMessage, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, true, fmt.Errorf("%s must be an object", key)
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, true, fmt.Errorf("%s must be an object", key)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, fmt.Errorf("%s must be an object", key)
	}
	return json.RawMessage(trimmed), true, nil
}

// getUUIDField reads a required UUID field.
func getUUIDField(fields map[string]json.RawMessage, key string) (uuid.UUID, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return uuid.Nil, false, nil
	}
	if isJSONNull(raw) {
		return uuid.Nil, true, fmt.Errorf("%s must be a UUID", key)
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return uuid.Nil, true, fmt.Errorf("%s must be a UUID", key)
	}
	return id, true, nil
}

// decodeTextChange decodes a full text operation object.
func decodeTextChange(raw json.RawMessage, name string) (TextChange, error) {
	fields, err := decodeObjectFields(raw, name)
	if err != nil {
		return TextChange{}, err
	}
	textOperation, ok, err := getStringField(fields, "textOperation")
	if err != nil {
		return TextChange{}, err
	}
	if !ok {
		return TextChange{}, errors.New("textOperation is required")
	}
	text, ok, err := getStringField(fields, "text")
	if err != nil {
		return TextChange{}, err
	}
	if !ok {
		return TextChange{}, errors.New("text is required")
	}
	index, ok, err := getIntField(fields, "index")
	if err != nil {
		return TextChange{}, err
	}
	if !ok {
		return TextChange{}, errors.New("index is required")
	}
	return TextChange{
		TextOperation: textOperation,
		Index:         index,
		Text:          text,
	}, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// validateOperationShape checks operation-level invariants before any database
// rows are locked or mutated.
func validateOperationShape(op ClientOperation) error {
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

// validateBlockType enforces the block type allow-list.
func validateBlockType(blockType string) error {
	if _, ok := allowedBlockTypes[blockType]; !ok {
		return fmt.Errorf("blockType %q is unsupported", blockType)
	}
	return nil
}

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
	// Note operation names.
	OperationCreateNote         = "create_note"
	OperationDeleteNote         = "delete_note"
	OperationModifyNoteProperty = "modify_note_property"
	OperationModifyNoteTitle    = "modify_note_title"

	// Block operation names.
	OperationCreateBlock         = "create_block"
	OperationUpdateBlock         = "update_block"
	OperationDeleteBlock         = "delete_block"
	OperationModifyBlockProperty = "modify_block_property"

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
// nested update_block.textDelta payload.
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

// normalizeOperationType accepts the client enum-style names while storing and
// dispatching through the existing snake_case operation names.
func normalizeOperationType(operationType string) string {
	switch operationType {
	case "CreateNote":
		return OperationCreateNote
	case "DeleteNote":
		return OperationDeleteNote
	case "ModifyNoteProperty":
		return OperationModifyNoteProperty
	case "ModifyNoteTitle":
		return OperationModifyNoteTitle
	case "CreateBlock":
		return OperationCreateBlock
	case "UpdateBlock":
		return OperationUpdateBlock
	case "DeleteBlock":
		return OperationDeleteBlock
	case "ModifyBlockProperty":
		return OperationModifyBlockProperty
	default:
		return operationType
	}
}

// // decodeChangeObject extracts the direct object payload from changeData.
func decodeChangeObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	return decodeObjectFields(raw, "changeData")
}

// decodeObjectFields validates and decodes a JSON object into raw field values.
func decodeObjectFields(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)

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

// getNullableObjectFields reads an optional object field. A JSON null value is
// treated the same as omission for operation fields documented as nullable.
func getNullableObjectFields(fields map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool, error) {
	raw, ok := fields[key]
	if !ok || isJSONNull(raw) {
		return nil, false, nil
	}
	value, err := decodeObjectFields(raw, key)
	return value, true, err
}

// getNullableUUIDField reads an optional UUID field that can explicitly be null.
func getNullableUUIDField(fields map[string]json.RawMessage, key string) (pgtype.UUID, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return pgtype.UUID{}, false, nil
	}
	if isJSONNull(raw) {
		return pgtype.UUID{}, true, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return pgtype.UUID{}, true, fmt.Errorf("%s must be a UUID or null", key)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, true, nil
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

// getNullableTextChangeField reads an optional nested text operation object.
func getNullableTextChangeField(fields map[string]json.RawMessage, key string) (TextChange, bool, error) {
	raw, ok := fields[key]
	if !ok || isJSONNull(raw) {
		return TextChange{}, false, nil
	}
	value, err := decodeTextChange(raw, key)
	return value, true, err
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
	if op.NoteID == uuid.Nil {
		return errors.New("noteId is required")
	}
	if op.Sequence < 0 {
		return errors.New("sequence must be greater than or equal to zero")
	}
	switch normalizeOperationType(op.OperationType) {
	case OperationCreateNote, OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
	case OperationCreateBlock, OperationUpdateBlock, OperationDeleteBlock, OperationModifyBlockProperty:
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

package syncapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestValidateOperationShapeRequiresBlockID protects the rule that every block
// operation must name the target block.
func TestValidateOperationShapeRequiresBlockID(t *testing.T) {
	op := ClientOperation{
		OperationID:   uuid.New(),
		NoteID:        uuid.New(),
		EntityType:    EntityBlock,
		OperationType: OperationUpdateBlock,
	}
	if err := validateOperationShape(op); err == nil {
		t.Fatal("expected missing blockId to fail validation")
	}
}

// TestDecodeFieldsRejectsUnsupportedSchemaVersion ensures clients cannot submit
// future/unknown change schemas silently.
func TestDecodeFieldsRejectsUnsupportedSchemaVersion(t *testing.T) {
	raw := json.RawMessage(`{"schemaVersion":2,"fields":{"title":"x"}}`)
	if _, err := decodeFields(raw); err == nil {
		t.Fatal("expected unsupported change schema version to fail")
	}
}

// TestNextCursorUsesLastPulledChangeWhenHasMore prevents pagination gaps when a
// response is only one page of a larger pull result.
func TestNextCursorUsesLastPulledChangeWhenHasMore(t *testing.T) {
	accepted := []AcceptedOperation{{Sequence: 50}}
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, accepted, changes, true)
	if got != 12 {
		t.Fatalf("next cursor = %d, want 12", got)
	}
}

// TestNextCursorIncludesAcceptedSequenceWhenPullIsExhausted allows the cursor to
// advance past locally accepted operations when no pull page remains.
func TestNextCursorIncludesAcceptedSequenceWhenPullIsExhausted(t *testing.T) {
	accepted := []AcceptedOperation{{Sequence: 50}}
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, accepted, changes, false)
	if got != 50 {
		t.Fatalf("next cursor = %d, want 50", got)
	}
}

// TestValidateBlockType protects the initial block type allow-list.
func TestValidateBlockType(t *testing.T) {
	for _, blockType := range []string{"text", "bullet", "todo", "numbered_list", "attachment"} {
		if err := validateBlockType(blockType); err != nil {
			t.Fatalf("expected %q to be valid: %v", blockType, err)
		}
	}
	if err := validateBlockType("spreadsheet"); err == nil {
		t.Fatal("expected unknown block type to fail")
	}
}

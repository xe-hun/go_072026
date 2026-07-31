package syncapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

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

func TestDecodeFieldsRejectsUnsupportedSchemaVersion(t *testing.T) {
	raw := json.RawMessage(`{"schemaVersion":2,"fields":{"title":"x"}}`)
	if _, err := decodeFields(raw); err == nil {
		t.Fatal("expected unsupported change schema version to fail")
	}
}

func TestNextCursorUsesLastPulledChangeWhenHasMore(t *testing.T) {
	accepted := []AcceptedOperation{{Sequence: 50}}
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, accepted, changes, true)
	if got != 12 {
		t.Fatalf("next cursor = %d, want 12", got)
	}
}

func TestNextCursorIncludesAcceptedSequenceWhenPullIsExhausted(t *testing.T) {
	accepted := []AcceptedOperation{{Sequence: 50}}
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, accepted, changes, false)
	if got != 50 {
		t.Fatalf("next cursor = %d, want 50", got)
	}
}

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

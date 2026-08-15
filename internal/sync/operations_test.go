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
		OperationType: OperationUpdateBlock,
	}
	if err := validateOperationShape(op); err == nil {
		t.Fatal("expected missing blockId to fail validation")
	}
}

// TestValidateOperationShapeRequiresNonNegativeSequence protects client
// ordering from values that cannot be sorted meaningfully.
func TestValidateOperationShapeRequiresNonNegativeSequence(t *testing.T) {
	op := ClientOperation{
		OperationID:     uuid.New(),
		NoteID:          uuid.New(),
		OperationType:   OperationUpdateNote,
		Sequence:        -1,
		BaseNoteVersion: 1,
	}
	if err := validateOperationShape(op); err == nil {
		t.Fatal("expected negative sequence to fail validation")
	}
}

// TestValidateOperationShapeDoesNotRequireEntityType keeps operation type as
// the single client-provided discriminator.
func TestValidateOperationShapeDoesNotRequireEntityType(t *testing.T) {
	op := ClientOperation{
		OperationID:     uuid.New(),
		NoteID:          uuid.New(),
		OperationType:   OperationUpdateNote,
		BaseNoteVersion: 1,
	}
	if err := validateOperationShape(op); err != nil {
		t.Fatalf("expected operation without entityType to pass validation: %v", err)
	}
}

// TestSortedOperationsOrdersByNoteThenSequence keeps batch application
// deterministic regardless of request order.
func TestSortedOperationsOrdersByNoteThenSequence(t *testing.T) {
	noteA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	noteB := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	ops := []ClientOperation{
		{OperationID: uuid.New(), NoteID: noteB, Sequence: 2},
		{OperationID: uuid.New(), NoteID: noteA, Sequence: 3},
		{OperationID: uuid.New(), NoteID: noteA, Sequence: 1},
		{OperationID: uuid.New(), NoteID: noteB, Sequence: 1},
	}

	got := sortedOperations(ops)
	wantNoteOrder := []uuid.UUID{noteA, noteA, noteB, noteB}
	wantSequenceOrder := []int{1, 3, 1, 2}
	for i := range got {
		if got[i].NoteID != wantNoteOrder[i] || got[i].Sequence != wantSequenceOrder[i] {
			t.Fatalf("sorted[%d] = (%s, %d), want (%s, %d)", i, got[i].NoteID, got[i].Sequence, wantNoteOrder[i], wantSequenceOrder[i])
		}
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

// TestApplyTextDeltaInsertAndDelete verifies the update_block delta semantics.
func TestApplyTextDeltaInsertAndDelete(t *testing.T) {
	inserted, err := applyTextDelta("helo", "l", TextOperationInsert, 2)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != "hello" {
		t.Fatalf("inserted text = %q, want hello", inserted)
	}

	deleted, err := applyTextDelta("hello", "ll", TextOperationDelete, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != "heo" {
		t.Fatalf("deleted text = %q, want heo", deleted)
	}
}

// TestApplyTextDeltaUsesRuneIndexes avoids corrupting multibyte text.
func TestApplyTextDeltaUsesRuneIndexes(t *testing.T) {
	got, err := applyTextDelta("hllo", "é", TextOperationInsert, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "héllo" {
		t.Fatalf("text = %q, want héllo", got)
	}
}

// TestApplyTextDeltaRejectsInvalidDeleteRange protects the current block text
// from out-of-range client indexes.
func TestApplyTextDeltaRejectsInvalidDeleteRange(t *testing.T) {
	if _, err := applyTextDelta("hi", "long", TextOperationDelete, 1); err == nil {
		t.Fatal("expected invalid delete range to fail")
	}
}

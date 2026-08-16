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
		OperationType: OperationModifyBlockProperty,
	}
	if err := validateOperationShape(op); err == nil {
		t.Fatal("expected missing blockId to fail validation")
	}
}

// TestValidateOperationShapeAcceptsNewOperationTypes protects the note/block
// operation names used by the direct changeData payloads.
func TestValidateOperationShapeAcceptsNewOperationTypes(t *testing.T) {
	blockID := uuid.New()
	categoryOperations := []string{OperationCreateCategory, OperationDeleteCategory, OperationModifyCategory}
	for _, operationType := range categoryOperations {
		if err := validateOperationShape(ClientOperation{
			OperationID:   uuid.New(),
			OperationType: operationType,
		}); err != nil {
			t.Fatalf("expected %s to pass validation: %v", operationType, err)
		}
	}
	ops := []ClientOperation{
		{OperationID: uuid.New(), NoteID: uuid.New(), OperationType: OperationModifyNoteProperty},
		{OperationID: uuid.New(), NoteID: uuid.New(), OperationType: OperationModifyNoteTitle},
		{OperationID: uuid.New(), NoteID: uuid.New(), BlockID: &blockID, OperationType: OperationModifyBlockProperty},
	}
	for _, op := range ops {
		if err := validateOperationShape(op); err != nil {
			t.Fatalf("expected %s to pass validation: %v", op.OperationType, err)
		}
	}
}

func TestValidateOperationShapeRejectsNoteIDForCategory(t *testing.T) {
	if err := validateOperationShape(ClientOperation{
		OperationID:   uuid.New(),
		NoteID:        uuid.New(),
		OperationType: OperationCreateCategory,
	}); err == nil {
		t.Fatal("expected category operation with noteId to fail validation")
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

// TestDecodeChangeObjectRejectsNonObject ensures operation payloads use the
// direct object shape.
func TestDecodeChangeObjectRejectsNonObject(t *testing.T) {
	raw := json.RawMessage(`[]`)
	if _, err := decodeChangeObject(raw); err == nil {
		t.Fatal("expected non-object changeData to fail")
	}
}

// TestDecodeTextChangeRequiresDirectShape verifies the text delta object
// expected by modify_note_title and modify_block_property.
func TestDecodeTextChangeRequiresDirectShape(t *testing.T) {
	change, err := decodeTextChange(json.RawMessage(`{"textOperation":"insert","index":1,"text":"e"}`), "changeData")
	if err != nil {
		t.Fatal(err)
	}
	if change.TextOperation != TextOperationInsert || change.Index != 1 || change.Text != "e" {
		t.Fatalf("unexpected text change: %+v", change)
	}
}

// TestNextCursorUsesLastPulledChangeWhenHasMore prevents pagination gaps when a
// response is only one page of a larger pull result.
func TestNextCursorUsesLastPulledChangeWhenHasMore(t *testing.T) {
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, changes, true)
	if got != 12 {
		t.Fatalf("next cursor = %d, want 12", got)
	}
}

// TestNextCursorIncludesPulledSequenceWhenPullIsExhausted advances the cursor
// through changes returned in the response.
func TestNextCursorIncludesPulledSequenceWhenPullIsExhausted(t *testing.T) {
	changes := []PulledChange{{Sequence: 11}, {Sequence: 12}}
	got := nextCursor(10, changes, false)
	if got != 12 {
		t.Fatalf("next cursor = %d, want 12", got)
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

// TestApplyTextDeltaInsertAndDelete verifies text delta semantics.
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

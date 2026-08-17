package syncapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNoteModelFromRequestBuildsNoteEntity(t *testing.T) {
	noteID := uuid.New()
	ownerID := uuid.New()
	blockID := uuid.New()
	op := ClientOperation{
		OperationID:   uuid.New(),
		NoteID:        noteID,
		OperationType: OperationCreateNote,
		ChangeData: json.RawMessage(`{
			"title":"Shopping",
			"noteProperties":{"isPinned":true},
			"blocks":[{
				"id":"` + blockID.String() + `",
				"blockType":"todo",
				"position":1.5,
				"textContent":"Buy milk",
				"blockProperties":{"isChecked":false}
			}]
		}`),
	}

	var model NoteModel
	if err := model.FromRequest(op, ownerID); err != nil {
		t.Fatal(err)
	}
	document, err := model.Entity()
	if err != nil {
		t.Fatal(err)
	}
	if document.Note.ID != noteID || document.Note.OwnerID != ownerID || len(document.Blocks) != 1 {
		t.Fatalf("unexpected note document: %+v", document)
	}
	if document.Blocks[0].ID != blockID || document.Blocks[0].Position != "1.5" {
		t.Fatalf("unexpected block entity: %+v", document.Blocks[0])
	}
}

func TestOperationFromRequestValidatesEnvelopeAndPayload(t *testing.T) {
	var model Operation
	err := model.FromRequest(ClientOperation{
		OperationID:   uuid.New(),
		NoteID:        uuid.New(),
		OperationType: OperationDeleteBlock,
		ChangeData:    json.RawMessage(`{"id":"not-a-uuid"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Fields) != 1 {
		t.Fatalf("decoded fields = %d, want 1", len(model.Fields))
	}
}

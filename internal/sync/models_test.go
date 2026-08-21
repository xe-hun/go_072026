package syncapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSyncResponseContracts(t *testing.T) {
	pushJSON, err := json.Marshal(Response{Accepted: []AcceptedDTO{}, Rejected: []RejectedDTO{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"changes", "hasMore", "nextCursor"} {
		if strings.Contains(string(pushJSON), `"`+field+`"`) {
			t.Fatalf("POST response unexpectedly contains %q: %s", field, pushJSON)
		}
	}

	pullJSON, err := json.Marshal(PulledChange{GlobalSequence: 42, ResultingNoteVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pullJSON), `"globalSequence":42`) {
		t.Fatalf("pulled change does not contain globalSequence: %s", pullJSON)
	}
	if !strings.Contains(string(pullJSON), `"resultingNoteVersion":7`) {
		t.Fatalf("pulled change does not contain resultingNoteVersion: %s", pullJSON)
	}
	for _, field := range []string{"operationId", "blockId", "sequence"} {
		if strings.Contains(string(pullJSON), `"`+field+`"`) {
			t.Fatalf("pulled change unexpectedly contains %q: %s", field, pullJSON)
		}
	}
}

func TestPullRequestValidate(t *testing.T) {
	valid := PullRequest{ProtocolVersion: ProtocolVersion, DeviceID: uuid.New(), Cursor: 0}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.Cursor = -1
	if err := valid.Validate(); err == nil {
		t.Fatal("expected a negative cursor to be rejected")
	}
}

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

func TestBlockModelFromCreateOperationUsesBlockField(t *testing.T) {
	blockID := uuid.New()
	var model BlockModel
	err := model.FromCreateOperation(json.RawMessage(`{
		"block": {
			"id":"` + blockID.String() + `",
			"blockType":"text",
			"position":1,
			"textContent":"hello",
			"blockProperties":{}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != blockID || model.TextContent != "hello" {
		t.Fatalf("unexpected block model: %+v", model)
	}
}

func TestBlockMutationFromRequestUsesChangedPropertiesAndTextDelta(t *testing.T) {
	blockID := uuid.New()
	var mutation BlockMutation
	err := mutation.FromRequest(json.RawMessage(`{
		"id":"`+blockID.String()+`",
		"changedProperties":{"checked":true},
		"textDelta":{"textOperation":"insert","index":0,"text":"x"}
	}`), OperationModifyBlock)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.ID != blockID || !mutation.HasProperties || !mutation.HasTextChange {
		t.Fatalf("unexpected block mutation: %+v", mutation)
	}
	if string(mutation.ChangedProperties["checked"]) != "true" {
		t.Fatalf("changedProperties not decoded: %s", mutation.ChangedProperties["checked"])
	}
}

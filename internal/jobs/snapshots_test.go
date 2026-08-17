package jobs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"notes-server/internal/store"
)

// TestEncodeSnapshotProducesStableChecksum verifies checksum generation is
// deterministic for the same snapshot document.
func TestEncodeSnapshotProducesStableChecksum(t *testing.T) {
	doc := SnapshotDocument{
		SchemaVersion: 1,
		Note: SnapshotNote{
			ID:             uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Title:          "Shopping",
			NoteProperties: json.RawMessage(`{"isPinned":true}`),
			Version:        3,
		},
		Blocks: []SnapshotBlock{
			{
				ID:              uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				Type:            "todo",
				Text:            "Buy milk",
				Position:        "a0",
				BlockProperties: json.RawMessage(`{"isChecked":false}`),
			},
		},
	}

	_, first, err := EncodeSnapshot(doc)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := EncodeSnapshot(doc)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("checksum should be stable, got %q and %q", first, second)
	}
}

// TestBuildSnapshotDocumentIncludesDeletedBlocks protects tombstone preservation
// in full-note snapshots.
func TestBuildSnapshotDocumentIncludesDeletedBlocks(t *testing.T) {
	deletedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	doc := store.NoteDocument{
		Note: store.Note{
			ID:             uuid.New(),
			OwnerID:        uuid.New(),
			Title:          "Archive",
			NoteProperties: json.RawMessage(`{}`),
			CurrentVersion: 2,
		},
		Blocks: []store.NoteBlock{
			{
				ID:              uuid.New(),
				NoteID:          uuid.New(),
				BlockType:       "text",
				TextContent:     "removed",
				Position:        "a0",
				BlockProperties: json.RawMessage(`{}`),
				DeletedAt:       pgtype.Timestamptz{Time: deletedAt, Valid: true},
			},
		},
	}

	snapshot := BuildSnapshotDocument(doc)
	if len(snapshot.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(snapshot.Blocks))
	}
	if snapshot.Blocks[0].DeletedAt == nil {
		t.Fatal("expected deleted block tombstone timestamp in snapshot")
	}
}

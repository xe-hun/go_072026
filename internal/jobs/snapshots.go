package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"notes-server/internal/store"
)

// SnapshotFormatV1 is the persisted snapshot document format name.
const SnapshotFormatV1 = "note-snapshot-v1"

// SnapshotDocument is the full JSON document stored in note_snapshots.
type SnapshotDocument struct {
	// SchemaVersion allows future JSON shape migrations.
	SchemaVersion int `json:"schemaVersion"`
	// Note stores note-level state.
	Note SnapshotNote `json:"note"`
	// Blocks stores ordered block state, including tombstones.
	Blocks []SnapshotBlock `json:"blocks"`
}

// SnapshotNote is the note-level portion of a snapshot document.
type SnapshotNote struct {
	// ID is the note UUID.
	ID uuid.UUID `json:"id"`
	// Title is the note title at snapshot time.
	Title string `json:"title"`
	// CategoryID is optional.
	CategoryID *uuid.UUID `json:"categoryId"`
	// Metadata stores note-level JSONB data.
	Metadata json.RawMessage `json:"metadata"`
	// Version is the note version represented by this snapshot.
	Version int64 `json:"version"`
	// DeletedAt is encoded as a string so the snapshot JSON is provider-neutral.
	DeletedAt *string `json:"deletedAt"`
}

// SnapshotBlock is the block-level portion of a snapshot document.
type SnapshotBlock struct {
	// ID is the block UUID.
	ID uuid.UUID `json:"id"`
	// Type is the block type string.
	Type string `json:"type"`
	// Text is the block text content.
	Text string `json:"text"`
	// Position is the fractional ordering key.
	Position string `json:"position"`
	// Properties stores block-type-specific JSONB data.
	Properties json.RawMessage `json:"properties"`
	// Version is the block version represented by this snapshot.
	Version int64 `json:"version"`
	// DeletedAt is present for block tombstones.
	DeletedAt *string `json:"deletedAt"`
}

// CreateSnapshot reads current note state, verifies it satisfies the job's
// expected version, builds the snapshot JSON, computes its checksum, and stores
// it in the same transaction supplied by the worker.
func CreateSnapshot(ctx context.Context, tx *store.Store, payload store.SnapshotJobPayload) (store.NoteSnapshot, error) {
	doc, err := tx.GetNoteDocument(ctx, payload.NoteID, payload.OwnerID)
	if err != nil {
		return store.NoteSnapshot{}, err
	}
	if doc.Note.CurrentVersion < payload.ExpectedVersion {
		// A snapshot job should never represent a future version. This check keeps
		// corrupted/stale jobs from writing misleading snapshots.
		return store.NoteSnapshot{}, errors.New("note version is behind snapshot job expectation")
	}

	snapshotDoc := BuildSnapshotDocument(doc)
	encoded, checksum, err := EncodeSnapshot(snapshotDoc)
	if err != nil {
		return store.NoteSnapshot{}, err
	}
	return tx.InsertSnapshot(ctx, store.NoteSnapshot{
		ID:             uuid.New(),
		NoteID:         doc.Note.ID,
		OwnerID:        doc.Note.OwnerID,
		Version:        doc.Note.CurrentVersion,
		SnapshotFormat: SnapshotFormatV1,
		SchemaVersion:  1,
		SnapshotData:   encoded,
		Checksum:       checksum,
	})
}

// BuildSnapshotDocument converts the current store document into the stable JSON
// snapshot shape.
func BuildSnapshotDocument(doc store.NoteDocument) SnapshotDocument {
	deletedAt := formatTimePtr(store.TimePtr(doc.Note.DeletedAt))
	blocks := make([]SnapshotBlock, 0, len(doc.Blocks))
	for _, block := range doc.Blocks {
		blocks = append(blocks, SnapshotBlock{
			ID:         block.ID,
			Type:       block.BlockType,
			Text:       block.TextContent,
			Position:   block.Position,
			Properties: store.NormalizeJSON(block.Properties),
			Version:    block.CurrentVersion,
			DeletedAt:  formatTimePtr(store.TimePtr(block.DeletedAt)),
		})
	}
	return SnapshotDocument{
		SchemaVersion: 1,
		Note: SnapshotNote{
			ID:         doc.Note.ID,
			Title:      doc.Note.Title,
			CategoryID: store.UUIDPtr(doc.Note.CategoryID),
			Metadata:   store.NormalizeJSON(doc.Note.Metadata),
			Version:    doc.Note.CurrentVersion,
			DeletedAt:  deletedAt,
		},
		Blocks: blocks,
	}
}

// EncodeSnapshot marshals the snapshot and computes a SHA-256 checksum over the
// exact bytes stored in PostgreSQL.
func EncodeSnapshot(doc SnapshotDocument) (json.RawMessage, string, error) {
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(sum[:]), nil
}

// formatTimePtr converts optional times to RFC3339-like strings for snapshots.
func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format("2006-01-02T15:04:05.999999999Z07:00")
	return &formatted
}

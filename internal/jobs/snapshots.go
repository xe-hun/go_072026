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

const SnapshotFormatV1 = "note-snapshot-v1"

type SnapshotDocument struct {
	SchemaVersion int             `json:"schemaVersion"`
	Note          SnapshotNote    `json:"note"`
	Blocks        []SnapshotBlock `json:"blocks"`
}

type SnapshotNote struct {
	ID         uuid.UUID       `json:"id"`
	Title      string          `json:"title"`
	CategoryID *uuid.UUID      `json:"categoryId"`
	Metadata   json.RawMessage `json:"metadata"`
	Version    int64           `json:"version"`
	DeletedAt  *string         `json:"deletedAt"`
}

type SnapshotBlock struct {
	ID         uuid.UUID       `json:"id"`
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	Position   string          `json:"position"`
	Properties json.RawMessage `json:"properties"`
	Version    int64           `json:"version"`
	DeletedAt  *string         `json:"deletedAt"`
}

func CreateSnapshot(ctx context.Context, tx *store.Store, payload store.SnapshotJobPayload) (store.NoteSnapshot, error) {
	doc, err := tx.GetNoteDocument(ctx, payload.NoteID, payload.OwnerID)
	if err != nil {
		return store.NoteSnapshot{}, err
	}
	if doc.Note.CurrentVersion < payload.ExpectedVersion {
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

func EncodeSnapshot(doc SnapshotDocument) (json.RawMessage, string, error) {
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(sum[:]), nil
}

func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format("2006-01-02T15:04:05.999999999Z07:00")
	return &formatted
}

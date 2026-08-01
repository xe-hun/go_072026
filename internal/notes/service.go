package notes

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Service contains note read/recovery rules. The sync service remains the main
// write path.
type Service struct {
	store *store.Store
}

// NewService wires note read services to the persistence boundary.
func NewService(store *store.Store) *Service {
	return &Service{store: store}
}

// Get returns a user-owned note and all of its blocks. The ownerID argument
// comes from the authenticated JWT, not from a client-supplied field.
func (s *Service) Get(ctx context.Context, ownerID, noteID uuid.UUID) (NoteResponse, error) {
	doc, err := s.store.GetNoteDocument(ctx, noteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return NoteResponse{}, httpapi.NotFound("Note not found.")
	}
	if err != nil {
		return NoteResponse{}, err
	}
	return mapNoteDocument(doc), nil
}

// LatestSnapshot returns the newest stored snapshot for a note.
func (s *Service) LatestSnapshot(ctx context.Context, ownerID, noteID uuid.UUID) (SnapshotResponse, error) {
	snapshot, err := s.store.GetLatestSnapshotForNote(ctx, noteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return SnapshotResponse{}, httpapi.NotFound("Snapshot not found.")
	}
	if err != nil {
		return SnapshotResponse{}, err
	}
	return SnapshotResponse{
		ID:             snapshot.ID,
		NoteID:         snapshot.NoteID,
		OwnerID:        snapshot.OwnerID,
		Version:        snapshot.Version,
		SnapshotFormat: snapshot.SnapshotFormat,
		SchemaVersion:  snapshot.SchemaVersion,
		SnapshotData:   snapshot.SnapshotData,
		Checksum:       snapshot.Checksum,
		CreatedAt:      snapshot.CreatedAt,
	}, nil
}

// mapNoteDocument converts the store's database-oriented note document into the
// public JSON response shape.
func mapNoteDocument(doc store.NoteDocument) NoteResponse {
	blocks := make([]BlockResponse, 0, len(doc.Blocks))
	for _, block := range doc.Blocks {
		blocks = append(blocks, BlockResponse{
			ID:             block.ID,
			NoteID:         block.NoteID,
			BlockType:      block.BlockType,
			TextContent:    block.TextContent,
			Position:       block.Position,
			Properties:     store.NormalizeJSON(block.Properties),
			CurrentVersion: block.CurrentVersion,
			CreatedAt:      block.CreatedAt,
			UpdatedAt:      block.UpdatedAt,
			DeletedAt:      store.TimePtr(block.DeletedAt),
		})
	}
	return NoteResponse{
		ID:             doc.Note.ID,
		OwnerID:        doc.Note.OwnerID,
		CategoryID:     store.UUIDPtr(doc.Note.CategoryID),
		Title:          doc.Note.Title,
		Metadata:       store.NormalizeJSON(doc.Note.Metadata),
		CurrentVersion: doc.Note.CurrentVersion,
		CreatedAt:      doc.Note.CreatedAt,
		UpdatedAt:      doc.Note.UpdatedAt,
		DeletedAt:      store.TimePtr(doc.Note.DeletedAt),
		Blocks:         blocks,
	}
}

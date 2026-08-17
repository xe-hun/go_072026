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
	var response NoteResponse
	if err := response.FromEntity(doc); err != nil {
		return NoteResponse{}, err
	}
	return response, nil
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
	var response SnapshotResponse
	if err := response.FromEntity(snapshot); err != nil {
		return SnapshotResponse{}, err
	}
	return response, nil
}

package syncapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/http"
	constants "notes-server/internal"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// snapshot for each supplied note ID.
func (s *Service) SyncFullNote(ctx context.Context, ownerID uuid.UUID, req FullNoteRequest) ([]NoteSnapshot, error) {
	if !constants.ValidDeviceID(req.DeviceID) {
		return nil, httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required and must be 30 characters or fewer.")
	}
	if len(req.NoteIDs) == 0 {
		return nil, httpapi.InvalidRequest("noteIds must contain at least one note ID.")
	}
	if len(req.NoteIDs) > s.maxOperations {
		return nil, httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The full-note request contains too many note IDs.")
	}
	for _, noteID := range req.NoteIDs {
		if noteID == uuid.Nil {
			return nil, httpapi.InvalidRequest("noteIds must contain valid UUIDs.")
		}
	}

	device, err := s.store.GetDeviceForOwner(ctx, req.DeviceID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpapi.Forbidden("Device does not belong to the authenticated user.")
	}
	if err != nil {
		return nil, err
	}
	if device.RevokedAt.Valid {
		return nil, httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked.")
	}

	snapshots := make([]NoteSnapshot, 0, len(req.NoteIDs))
	for _, noteID := range req.NoteIDs {
		document, err := s.store.GetNoteDocument(ctx, noteID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, httpapi.NotFound("Note not found.")
		}
		if err != nil {
			return nil, err
		}
		var snapshot NoteSnapshot
		if err := snapshot.FromEntity(document); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// publishCommittedSyncAvailable publishes a best-effort hint after WithTx has
// returned successfully. Keeping this boundary in a separate helper makes the
// ordering explicit: a broker failure is logged, but can never undo or change

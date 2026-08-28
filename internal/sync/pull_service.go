package syncapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Pull returns remote changes after the supplied cursor for a device.
func (s *Service) Pull(ctx context.Context, ownerID uuid.UUID, req PullRequest) (PullResponse, error) {
	if err := req.Validate(); err != nil {
		return PullResponse{}, err
	}
	limit := s.defaultPullLimit
	if limit > s.maxPullLimit {
		limit = s.maxPullLimit
	}
	resp := PullResponse{Changes: []PulledChange{}, NextCursor: req.Cursor}
	err := s.store.WithTx(ctx, func(tx *store.Store) error {
		device, err := tx.GetDeviceForOwnerForUpdate(ctx, req.DeviceID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			return httpapi.Forbidden("Device does not belong to the authenticated user.")
		}
		if err != nil {
			return err
		}
		if device.RevokedAt.Valid {
			return httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked.")
		}
		changes, err := tx.GetChangesAfterCursor(ctx, ownerID, req.Cursor, req.DeviceID, limit+1)
		if err != nil {
			return err
		}
		if int32(len(changes)) > limit {
			resp.HasMore = true
			changes = changes[:limit]
		}
		for _, change := range changes {
			var pulled PulledChange
			if err := pulled.FromEntity(change); err != nil {
				return err
			}
			resp.Changes = append(resp.Changes, pulled)
		}
		resp.NextCursor = nextCursor(req.Cursor, resp.Changes)
		return tx.UpdateDeviceCursor(ctx, req.DeviceID, ownerID, resp.NextCursor)
	})
	return resp, err
}

// UpdatesAvailable reports whether a device has remote changes after its
// persisted sync cursor.
func (s *Service) UpdatesAvailable(ctx context.Context, ownerID uuid.UUID, req UpdatesAvailableRequest) (UpdatesAvailableResponse, error) {
	if err := req.Validate(); err != nil {
		return UpdatesAvailableResponse{}, err
	}
	device, err := s.store.GetDeviceForOwner(ctx, req.DeviceID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return UpdatesAvailableResponse{}, httpapi.Forbidden("Device does not belong to the authenticated user.")
	}
	if err != nil {
		return UpdatesAvailableResponse{}, err
	}
	if device.RevokedAt.Valid {
		return UpdatesAvailableResponse{}, httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked.")
	}
	available, err := s.store.HasChangesAfterCursor(ctx, ownerID, device.LastGlobalCursor, req.DeviceID)
	if err != nil {
		return UpdatesAvailableResponse{}, err
	}
	return UpdatesAvailableResponse{UpdatesAvailable: available}, nil
}

// sortedOperations returns a copy ordered by note id and then client sequence.

// returned pulled change so the next page is not skipped.
func nextCursor(start int64, changes []PulledChange) int64 {
	next := start
	for _, change := range changes {
		if change.GlobalSequence > next {
			next = change.GlobalSequence
		}
	}
	return next
}

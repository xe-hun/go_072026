package syncapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// request-level failures return an HTTP error.
func (s *Service) Sync(ctx context.Context, ownerID uuid.UUID, req Request) (Response, error) {
	if err := req.Validate(s.maxOperations); err != nil {
		return Response{}, err
	}

	resp := Response{
		Accepted: []AcceptedDTO{},
		Rejected: []RejectedDTO{},
	}
	var committedCursor int64

	err := s.store.WithTx(ctx, func(tx *store.Store) error {
		// Lock the device row so sync requests from the same device serialize.
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

		operations := sortedOperations(req.Operations)
		for start := 0; start < len(operations); {
			noteID := operations[start].NoteID
			end := start + 1
			for end < len(operations) && operations[end].NoteID == noteID {
				end++
			}

			// Each note batch is independently accepted or rejected. A rejected
			// operation rolls back the whole note batch without discarding other
			// note batches in the request.
			accepted, rejected, err := s.applyOperationBatch(ctx, tx, ownerID, req.DeviceID, operations[start:end])
			if err != nil {
				return err
			}
			if rejected != nil {
				resp.Rejected = append(resp.Rejected, *rejected)
			} else if accepted != nil {
				resp.Accepted = append(resp.Accepted, *accepted)
				if accepted.globalSequence > committedCursor {
					committedCursor = accepted.globalSequence
				}
			}
			start = end
		}

		return nil
	})
	if err != nil {
		return Response{}, err
	}
	s.publishCommittedSyncAvailable(ctx, ownerID, req.DeviceID, committedCursor)

	s.logger.InfoContext(ctx, "sync completed",
		"user_id", ownerID.String(),
		"device_id", req.DeviceID,
		"accepted", len(resp.Accepted),
		"rejected", len(resp.Rejected),
	)
	return resp, nil
}

// SyncFullNote validates the requesting device and returns the current note

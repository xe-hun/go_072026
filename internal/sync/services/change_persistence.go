package services

import (
	"context"
	"github.com/google/uuid"
	"notes-server/internal/store"
	. "notes-server/internal/sync/models"
)

// insertChange writes the append-only history row for an accepted operation.
func (s *Service) insertChange(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, op ClientOperation, blockID *uuid.UUID, baseNoteVersion, resultingNoteVersion int64) (store.NoteChange, error) {
	params, err := op.Entity(ownerID, deviceID, blockID, baseNoteVersion, resultingNoteVersion)
	if err != nil {
		return store.NoteChange{}, err
	}
	return tx.InsertNoteChange(ctx, params)
}

// enqueueSnapshotIfNeeded evaluates configured thresholds and inserts a snapshot
// outbox job when the note has enough unsnapshotted history.
func (s *Service) enqueueSnapshotIfNeeded(ctx context.Context, tx *store.Store, noteID, ownerID uuid.UUID, resultingVersion int64) error {
	if s.snapshotChangeThreshold <= 0 && s.snapshotByteThreshold <= 0 {
		return nil
	}
	changeCount, err := tx.CountChangesSinceLastSnapshot(ctx, noteID)
	if err != nil {
		return err
	}
	changeBytes, err := tx.SumChangeBytesSinceLastSnapshot(ctx, noteID)
	if err != nil {
		return err
	}
	if (s.snapshotChangeThreshold > 0 && changeCount >= s.snapshotChangeThreshold) ||
		(s.snapshotByteThreshold > 0 && changeBytes >= s.snapshotByteThreshold) {
		_, err := tx.EnqueueSnapshotJob(ctx, noteID, ownerID, resultingVersion)
		return err
	}
	return nil
}

// acceptedFromChange maps a stored change to the accepted response shape. It is
// used for both first-time accepts and idempotent retries.
func acceptedFromChange(change store.NoteChange) AcceptedDTO {
	return AcceptedDTO{
		NoteID:            change.NoteID,
		ServerNoteVersion: change.ResultingNoteVersion,
		GlobalSequence:    change.GlobalSequence,
	}
}

// nextCursor calculates the cursor the client should persist after this
// response. When a pull page has more rows, the cursor must stop at the last

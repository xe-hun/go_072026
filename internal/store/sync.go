package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "notes-server/db/generated"
)

// FindProcessedOperation looks up an operation by the idempotency key
// (device_id, client_operation_id).
func (s *Store) FindProcessedOperation(ctx context.Context, deviceID, operationID uuid.UUID) (NoteChange, error) {
	change, err := s.q.FindProcessedOperation(ctx, db.FindProcessedOperationParams{
		DeviceID:          pgUUID(deviceID),
		ClientOperationID: pgUUID(operationID),
	})
	return fromDBChange(change), mapNoRows(err)
}

// InsertNoteChange appends the immutable history row for an accepted operation.
func (s *Store) InsertNoteChange(ctx context.Context, arg InsertNoteChangeParams) (NoteChange, error) {
	blockID := pgtype.UUID{}
	if arg.BlockID != nil {
		// Note-level changes intentionally leave block_id NULL.
		blockID = pgUUID(*arg.BlockID)
	}
	change, err := s.q.InsertNoteChange(ctx, db.InsertNoteChangeParams{
		ID:                   pgUUID(arg.ID),
		OwnerID:              pgUUID(arg.OwnerID),
		NoteID:               pgUUID(arg.NoteID),
		BlockID:              blockID,
		DeviceID:             pgUUID(arg.DeviceID),
		ClientOperationID:    pgUUID(arg.ClientOperationID),
		EntityType:           arg.EntityType,
		OperationType:        arg.OperationType,
		BaseNoteVersion:      arg.BaseNoteVersion,
		ResultingNoteVersion: arg.ResultingNoteVersion,
		ChangeFormat:         arg.ChangeFormat,
		SchemaVersion:        arg.SchemaVersion,
		ChangeData:           []byte(NormalizeJSON(arg.ChangeData)),
	})
	return fromDBChange(change), err
}

// GetChangesAfterCursor returns remote changes after a cursor, excluding changes
// submitted by the requesting device.
func (s *Store) GetChangesAfterCursor(ctx context.Context, ownerID uuid.UUID, cursor int64, excludeDeviceID uuid.UUID, limit int32) ([]NoteChange, error) {
	rows, err := s.q.GetChangesAfterCursor(ctx, db.GetChangesAfterCursorParams{
		OwnerID:        pgUUID(ownerID),
		GlobalSequence: pgtype.Int8{Int64: cursor, Valid: true},
		DeviceID:       pgUUID(excludeDeviceID),
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}
	changes := make([]NoteChange, 0, len(rows))
	for _, row := range rows {
		changes = append(changes, fromDBChange(row))
	}
	return changes, nil
}

// CountChangesSinceLastSnapshot measures snapshot eligibility by change count.
func (s *Store) CountChangesSinceLastSnapshot(ctx context.Context, noteID uuid.UUID) (int64, error) {
	return s.q.CountChangesSinceLastSnapshot(ctx, pgUUID(noteID))
}

// SumChangeBytesSinceLastSnapshot measures snapshot eligibility by payload size.
func (s *Store) SumChangeBytesSinceLastSnapshot(ctx context.Context, noteID uuid.UUID) (int64, error) {
	return s.q.SumChangeBytesSinceLastSnapshot(ctx, pgUUID(noteID))
}

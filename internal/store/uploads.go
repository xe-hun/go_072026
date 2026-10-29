package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "notes-server/db/generated"
)

// CreateFileUpload inserts one pending upload record before a signed upload
// authorization is returned to the browser.
func (s *Store) CreateFileUpload(ctx context.Context, upload FileUpload) (FileUpload, error) {
	row, err := s.q.CreateFileUpload(ctx, db.CreateFileUploadParams{
		ID:               pgUUID(upload.ID),
		NoteID:           pgUUID(upload.NoteID),
		OwnerID:          pgUUID(upload.OwnerID),
		BlockID:          pgUUID(upload.BlockID),
		DeviceID:         pgtype.Text{String: upload.DeviceID, Valid: upload.DeviceID != ""},
		OriginalFilename: upload.OriginalFilename,
		StorageBucket:    upload.StorageBucket,
		StoragePath:      upload.StoragePath,
		ContentType:      upload.ContentType,
		ExpectedSize:     upload.ExpectedSize,
		ExpiresAt:        pgTime(upload.ExpiresAt),
	})
	return fromDBFileUpload(row), err
}

// ListFileUploadsAfterCursor returns a user's upload feed in cursor order.
// The service applies lifecycle and device filtering because pending uploads
// must not create a cursor gap that can hide a later completed upload.
func (s *Store) ListFileUploadsAfterCursor(ctx context.Context, ownerID uuid.UUID, cursor int64) ([]FileUpload, error) {
	rows, err := s.q.ListFileUploadsAfterCursor(ctx, db.ListFileUploadsAfterCursorParams{
		OwnerID: pgUUID(ownerID),
		Cursor:  pgtype.Int8{Int64: cursor, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	result := make([]FileUpload, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromDBFileUpload(row))
	}
	return result, nil
}

// GetFileUploadForOwner scopes an upload lookup to the authenticated user.
func (s *Store) GetFileUploadForOwner(ctx context.Context, uploadID, ownerID uuid.UUID) (FileUpload, error) {
	row, err := s.q.GetFileUploadForOwner(ctx, db.GetFileUploadForOwnerParams{
		ID:      pgUUID(uploadID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

// CountPendingFileUploads limits storage authorization abuse without counting
// expired rows that are waiting for worker reconciliation.
func (s *Store) CountPendingFileUploads(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return s.q.CountPendingFileUploads(ctx, pgUUID(ownerID))
}

// MarkFileUploadFailed records a safe, server-generated failure reason while
// keeping the upload path available for later cleanup.
func (s *Store) MarkFileUploadFailed(ctx context.Context, uploadID, ownerID uuid.UUID, message string) (FileUpload, error) {
	row, err := s.q.MarkFileUploadFailed(ctx, db.MarkFileUploadFailedParams{
		ID:           pgUUID(uploadID),
		OwnerID:      pgUUID(ownerID),
		ErrorMessage: pgtype.Text{String: message, Valid: message != ""},
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

// CompleteFileUpload conditionally changes only a pending upload. This atomic
// predicate is the concurrency boundary for idempotent completion retries.
func (s *Store) CompleteFileUpload(ctx context.Context, uploadID, ownerID uuid.UUID, actualSize int64, verifiedContentType string) (FileUpload, error) {
	row, err := s.q.CompleteFileUpload(ctx, db.CompleteFileUploadParams{
		ID:                  pgUUID(uploadID),
		OwnerID:             pgUUID(ownerID),
		ActualSize:          pgtype.Int8{Int64: actualSize, Valid: true},
		VerifiedContentType: pgtype.Text{String: verifiedContentType, Valid: verifiedContentType != ""},
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

// MarkFileUploadDeleted performs the durable half of an idempotent deletion.
func (s *Store) MarkFileUploadDeleted(ctx context.Context, uploadID, ownerID uuid.UUID) (FileUpload, error) {
	row, err := s.q.MarkFileUploadDeleted(ctx, db.MarkFileUploadDeletedParams{
		ID:      pgUUID(uploadID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

// MarkFileUploadCancelled reserves the lifecycle row before an external
// deletion. Completion can no longer win after this conditional transition.
func (s *Store) MarkFileUploadCancelled(ctx context.Context, uploadID, ownerID uuid.UUID) (FileUpload, error) {
	row, err := s.q.MarkFileUploadCancelled(ctx, db.MarkFileUploadCancelledParams{
		ID:      pgUUID(uploadID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

// ListExpiredFileUploads returns only uploads that have remained pending past
// the caller's grace-period cutoff.
func (s *Store) ListExpiredFileUploads(ctx context.Context, cutoff time.Time, limit int32) ([]FileUpload, error) {
	rows, err := s.q.ListExpiredFileUploads(ctx, db.ListExpiredFileUploadsParams{
		ExpiresAt: pgTime(cutoff),
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]FileUpload, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromDBFileUpload(row))
	}
	return result, nil
}

// MarkFileUploadExpired changes only pending rows so a late cleanup pass cannot
// overwrite a completion that won a race.
func (s *Store) MarkFileUploadExpired(ctx context.Context, uploadID uuid.UUID, message string) (FileUpload, error) {
	row, err := s.q.MarkFileUploadExpired(ctx, db.MarkFileUploadExpiredParams{
		ID:           pgUUID(uploadID),
		ErrorMessage: pgtype.Text{String: message, Valid: message != ""},
	})
	return fromDBFileUpload(row), mapNoRows(err)
}

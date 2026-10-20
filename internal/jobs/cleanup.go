package jobs

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"strings"
	"time"

	"notes-server/internal/storage"
	"notes-server/internal/store"
)

// Cleaner reconciles pending uploads only after their authorization expiry plus
// a grace period. That delay avoids deleting an object while a late browser
// request may still be finishing.
type Cleaner struct {
	store   *store.Store
	storage storage.Storage
	grace   time.Duration
	logger  *slog.Logger
}

// NewCleaner constructs the upload reconciliation helper used by the worker.
func NewCleaner(st *store.Store, storageClient storage.Storage, grace time.Duration, logger *slog.Logger) *Cleaner {
	return &Cleaner{store: st, storage: storageClient, grace: grace, logger: logger}
}

// RunOnce checks a bounded page of abandoned uploads. Existing objects are
// independently verified and finalized when their metadata matches; missing
// or invalid objects are cleaned up and marked expired.
func (c *Cleaner) RunOnce(ctx context.Context) error {
	uploads, err := c.store.ListExpiredFileUploads(ctx, time.Now().UTC().Add(-c.grace), 100)
	if err != nil {
		return err
	}
	var firstErr error
	for _, upload := range uploads {
		info, infoErr := c.storage.GetObjectInfo(ctx, upload.StorageBucket, upload.StoragePath)
		switch {
		case infoErr == nil && info.Size == upload.ExpectedSize && normalizedType(info.ContentType) == normalizedType(upload.ContentType):
			if _, completeErr := c.store.CompleteFileUpload(ctx, upload.ID, upload.OwnerID, info.Size, normalizedType(info.ContentType)); completeErr != nil && !errors.Is(completeErr, store.ErrNotFound) {
				firstErr = joinFirst(firstErr, completeErr)
			}
		case infoErr == nil:
			if deleteErr := c.storage.DeleteObject(ctx, upload.StorageBucket, upload.StoragePath); deleteErr != nil && !errors.Is(deleteErr, storage.ErrNotFound) {
				firstErr = joinFirst(firstErr, deleteErr)
				continue
			}
			if _, expireErr := c.store.MarkFileUploadExpired(ctx, upload.ID, "expired upload metadata did not match"); expireErr != nil && !errors.Is(expireErr, store.ErrNotFound) {
				firstErr = joinFirst(firstErr, expireErr)
			}
		case errors.Is(infoErr, storage.ErrNotFound):
			if _, expireErr := c.store.MarkFileUploadExpired(ctx, upload.ID, "upload authorization expired before an object was stored"); expireErr != nil && !errors.Is(expireErr, store.ErrNotFound) {
				firstErr = joinFirst(firstErr, expireErr)
			}
		default:
			firstErr = joinFirst(firstErr, infoErr)
		}
	}
	return firstErr
}

func normalizedType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(mediaType)
}

func joinFirst(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

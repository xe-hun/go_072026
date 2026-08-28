package syncapi

import (
	"context"
	"github.com/google/uuid"
	"notes-server/internal/realtime"
)

// the result of the already-committed sync transaction.
func (s *Service) publishCommittedSyncAvailable(ctx context.Context, ownerID uuid.UUID, deviceID string, latestCursor int64) {
	if latestCursor <= 0 || s.eventBus == nil {
		return
	}
	event := realtime.SyncAvailableEvent{
		UserID:         ownerID,
		OriginDeviceID: deviceID,
		LatestCursor:   latestCursor,
	}
	if err := s.eventBus.PublishSyncAvailable(ctx, event); err != nil {
		s.logger.ErrorContext(ctx, "notification publish failed",
			"user_id", ownerID,
			"device_id", deviceID,
			"latest_cursor", latestCursor,
			"error", err,
		)
	}
}

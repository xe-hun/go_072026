package syncapi

import (
	"errors"
	"log/slog"

	"notes-server/internal/config"
	"notes-server/internal/realtime"
	"notes-server/internal/store"
)

var errOperationBatchRejected = errors.New("operation batch rejected")

// Service owns sync business rules. It is deliberately above the store layer so
// transactions can include both current-state writes and change-log inserts.
type Service struct {
	// store is the persistence boundary.
	store *store.Store
	// logger records aggregate sync outcomes without logging note content.
	logger *slog.Logger
	// maxOperations bounds batch size.
	maxOperations int
	// defaultPullLimit bounds one GET pull page.
	defaultPullLimit int32
	// maxPullLimit caps response size.
	maxPullLimit int32
	// snapshotChangeThreshold controls snapshot job enqueueing by count.
	snapshotChangeThreshold int64
	// snapshotByteThreshold controls snapshot job enqueueing by payload bytes.
	snapshotByteThreshold int64
	// eventBus publishes only after a successful sync transaction commits.
	eventBus realtime.EventBus
}

// NewService builds a sync service using validated runtime limits.
func NewService(store *store.Store, cfg config.Config, logger *slog.Logger, eventBus realtime.EventBus) *Service {
	return &Service{
		store:                   store,
		logger:                  logger,
		maxOperations:           cfg.SyncMaxOperations,
		defaultPullLimit:        int32(cfg.SyncDefaultPullLimit),
		maxPullLimit:            int32(cfg.SyncMaxPullLimit),
		snapshotChangeThreshold: cfg.SnapshotChangeThreshold,
		snapshotByteThreshold:   cfg.SnapshotByteThreshold,
		eventBus:                eventBus,
	}
}

// Sync applies client operations in one transaction.
// A valid request can contain accepted and rejected operations; only

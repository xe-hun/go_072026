package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"notes-server/internal/store"
)

// Worker polls the PostgreSQL outbox and processes jobs. It keeps no durable
// state in memory, so multiple worker processes can run concurrently.
type Worker struct {
	// store claims jobs and performs snapshot writes.
	store *store.Store
	// logger records job failures and lifecycle events.
	logger *slog.Logger
	// workerID is written into outbox_jobs.locked_by.
	workerID string
	// pollInterval controls how long to wait when no jobs are available.
	pollInterval time.Duration
	// lockTimeout lets another worker reclaim a stale locked job.
	lockTimeout time.Duration
}

// NewWorker constructs a worker and generates an ID when one is not supplied.
func NewWorker(store *store.Store, logger *slog.Logger, workerID string) *Worker {
	if workerID == "" {
		workerID = "worker-" + uuid.NewString()
	}
	return &Worker{
		store:        store,
		logger:       logger,
		workerID:     workerID,
		pollInterval: 2 * time.Second,
		lockTimeout:  2 * time.Minute,
	}
}

// Run loops until context cancellation. It processes available jobs immediately
// and sleeps only when the queue is empty.
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		worked, err := w.ProcessOnce(ctx)
		if err != nil {
			// Job failures are logged and stored on the outbox row, but the worker
			// process stays alive for future jobs.
			w.logger.ErrorContext(ctx, "worker iteration failed", "error", err)
		}
		if worked {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ProcessOnce claims and processes at most one job. The boolean reports whether
// a job was claimed.
func (w *Worker) ProcessOnce(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimOutboxJob(ctx, w.workerID, w.lockTimeout)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	err = w.processJob(ctx, job)
	if err != nil {
		// Failure releases the lock and schedules a retry with bounded backoff.
		_ = w.store.FailOutboxJob(ctx, job.ID, err.Error(), retryDelay(job.Attempts))
		return true, err
	}
	if err := w.store.CompleteOutboxJob(ctx, job.ID); err != nil {
		return true, err
	}
	return true, nil
}

// processJob dispatches by job type.
func (w *Worker) processJob(ctx context.Context, job store.OutboxJob) error {
	switch job.JobType {
	case store.JobCreateSnapshot:
		var payload store.SnapshotJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return err
		}
		// Snapshot reads and writes happen in one transaction so the snapshot row
		// matches one consistent view of note/block state.
		return w.store.WithTx(ctx, func(tx *store.Store) error {
			_, err := CreateSnapshot(ctx, tx, payload)
			return err
		})
	default:
		return errors.New("unsupported outbox job type")
	}
}

// retryDelay returns a simple bounded retry delay based on attempts.
func retryDelay(attempts int32) time.Duration {
	if attempts < 1 {
		return time.Second
	}
	delay := time.Duration(attempts) * 5 * time.Second
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

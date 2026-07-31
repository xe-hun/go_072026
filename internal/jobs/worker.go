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

type Worker struct {
	store        *store.Store
	logger       *slog.Logger
	workerID     string
	pollInterval time.Duration
	lockTimeout  time.Duration
}

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
		_ = w.store.FailOutboxJob(ctx, job.ID, err.Error(), retryDelay(job.Attempts))
		return true, err
	}
	if err := w.store.CompleteOutboxJob(ctx, job.ID); err != nil {
		return true, err
	}
	return true, nil
}

func (w *Worker) processJob(ctx context.Context, job store.OutboxJob) error {
	switch job.JobType {
	case store.JobCreateSnapshot:
		var payload store.SnapshotJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return err
		}
		return w.store.WithTx(ctx, func(tx *store.Store) error {
			_, err := CreateSnapshot(ctx, tx, payload)
			return err
		})
	default:
		return errors.New("unsupported outbox job type")
	}
}

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

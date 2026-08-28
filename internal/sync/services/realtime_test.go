package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"notes-server/internal/realtime"
)

type failingEventBus struct {
	calls int
}

func (b *failingEventBus) PublishSyncAvailable(context.Context, realtime.SyncAvailableEvent) error {
	b.calls++
	return errors.New("broker unavailable")
}

func (b *failingEventBus) Subscribe(context.Context, func(realtime.SyncAvailableEvent)) error {
	return nil
}

func TestCommittedRealtimePublishFailureIsBestEffort(t *testing.T) {
	eventBus := &failingEventBus{}
	service := &Service{
		eventBus: eventBus,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// The sync transaction has already returned successfully before this helper
	// is called. A broker failure must therefore be observable only through the
	// log, not as a failed sync result or panic.
	service.publishCommittedSyncAvailable(context.Background(), uuid.New(), "phone", 8425)
	if eventBus.calls != 1 {
		t.Fatalf("publish calls = %d, want 1", eventBus.calls)
	}
}

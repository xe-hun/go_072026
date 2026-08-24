//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"notes-server/internal/realtime"
)

func TestPostgresEventBusDeliversToLocalHub(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	hub := realtime.NewHub(slog.Default())
	userID := uuid.New()
	client := realtime.NewClient(userID, "tablet", nil)
	if err := hub.Register(client); err != nil {
		t.Fatal(err)
	}
	defer hub.Unregister(client)

	bus := realtime.NewPostgresEventBus(pool, slog.Default())
	listenerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenerDone := make(chan error, 1)
	go func() {
		listenerDone <- bus.Subscribe(listenerCtx, hub.PublishSyncAvailable)
	}()

	event := realtime.SyncAvailableEvent{
		UserID:         userID,
		OriginDeviceID: "phone",
		LatestCursor:   8425,
	}
	publishTicker := time.NewTicker(100 * time.Millisecond)
	defer publishTicker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()

	for {
		select {
		case message := <-client.Send:
			if message.LatestCursor != event.LatestCursor {
				t.Fatalf("cursor = %d, want %d", message.LatestCursor, event.LatestCursor)
			}
			cancel()
			select {
			case err := <-listenerDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("listener did not stop after cancellation")
			}
			return
		case <-publishTicker.C:
			if err := bus.PublishSyncAvailable(context.Background(), event); err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for PostgreSQL realtime notification")
		}
	}
}

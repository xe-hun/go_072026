package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	notificationChannel = "sync_available"
	initialBackoff      = 250 * time.Millisecond
	maxBackoff          = 5 * time.Second
)

// PostgresEventBus uses pg_notify for cross-instance delivery. Publishing uses
// the normal pool; subscribing acquires one dedicated connection because
// LISTEN is scoped to a PostgreSQL session.
type PostgresEventBus struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewPostgresEventBus creates a LISTEN/NOTIFY event bus over an existing pool.
func NewPostgresEventBus(pool *pgxpool.Pool, logger *slog.Logger) *PostgresEventBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &PostgresEventBus{pool: pool, logger: logger}
}

// PublishSyncAvailable sends a tiny JSON hint after the caller's transaction
// has already committed. A pg_notify failure is returned so the caller can log
// it, but it must never be used to roll back a completed sync.
func (b *PostgresEventBus) PublishSyncAvailable(ctx context.Context, event SyncAvailableEvent) error {
	if b.pool == nil {
		return errors.New("postgres realtime event bus has no pool")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", notificationChannel, string(payload))
	return err
}

// Subscribe continuously listens for events and reconnects with bounded
// exponential backoff. It returns nil when ctx is canceled during normal
// shutdown; malformed payloads are logged and ignored.
func (b *PostgresEventBus) Subscribe(ctx context.Context, handler func(SyncAvailableEvent)) error {
	if b.pool == nil {
		return errors.New("postgres realtime event bus has no pool")
	}
	if handler == nil {
		return errors.New("postgres realtime event handler is required")
	}

	backoff := initialBackoff
	connectedBefore := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		conn, err := b.pool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.logger.Warn("postgres listener disconnected", "error", err)
			if !waitBackoff(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}

		ready := false
		err = b.listen(ctx, conn, handler, func() {
			ready = true
			if connectedBefore {
				b.logger.Info("postgres listener reconnected")
			} else {
				b.logger.Info("postgres listener connected")
				connectedBefore = true
			}
		})
		releaseListenerConnection(conn)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			b.logger.Warn("postgres listener disconnected", "error", err)
		}
		delay := backoff
		if ready {
			backoff = initialBackoff
		} else {
			backoff = nextBackoff(backoff)
		}
		if !waitBackoff(ctx, delay) {
			return nil
		}
	}
}

// releaseListenerConnection removes the session-scoped subscription before
// returning the connection to pgxpool. The cleanup uses its own short-lived
// context because the listener context may already be canceled or the socket
// may already be broken.
func releaseListenerConnection(conn *pgxpool.Conn) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = conn.Exec(cleanupCtx, "UNLISTEN "+notificationChannel)
	conn.Release()
}

func (b *PostgresEventBus) listen(ctx context.Context, conn *pgxpool.Conn, handler func(SyncAvailableEvent), onConnected func()) error {
	if _, err := conn.Exec(ctx, "LISTEN "+notificationChannel); err != nil {
		return err
	}
	onConnected()
	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		event, err := decodeSyncAvailableEvent(notification.Payload)
		if err != nil {
			b.logger.Warn("malformed postgres realtime event ignored", "error", err)
			continue
		}
		handler(event)
	}
}

func decodeSyncAvailableEvent(payload string) (SyncAvailableEvent, error) {
	var event SyncAvailableEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return SyncAvailableEvent{}, err
	}
	if err := event.Validate(); err != nil {
		return SyncAvailableEvent{}, err
	}
	return event, nil
}

func waitBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

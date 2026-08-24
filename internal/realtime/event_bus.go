package realtime

import "context"

// EventBus isolates sync notifications from their transport. The sync service
// only knows that an event can be published; PostgreSQL LISTEN/NOTIFY can later
// be replaced without changing sync or WebSocket code.
type EventBus interface {
	PublishSyncAvailable(context.Context, SyncAvailableEvent) error
	Subscribe(context.Context, func(SyncAvailableEvent)) error
}

package realtime

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

var errHubClosed = errors.New("realtime hub is closed")

// Hub owns only active WebSocket connections on one WebSocket server instance. The mutex
// keeps registration, replacement, removal, and notification fan-out simple
// and makes the hot path a short map lookup plus bounded channel sends.
type Hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[string]*Client
	closed  bool
	wg      sync.WaitGroup
	logger  *slog.Logger
}

// NewHub creates an empty local connection hub.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		clients: make(map[uuid.UUID]map[string]*Client),
		logger:  logger,
	}
}

// Register adds a client. A second connection for the same user/device
// replaces the old one, preventing duplicate notifications after reconnects.
func (h *Hub) Register(client *Client) error {
	if client == nil || client.UserID == uuid.Nil || client.DeviceID == "" {
		return errors.New("realtime client identity is invalid")
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errHubClosed
	}
	userClients := h.clients[client.UserID]
	if userClients == nil {
		userClients = make(map[string]*Client)
		h.clients[client.UserID] = userClients
	}
	previous := userClients[client.DeviceID]
	userClients[client.DeviceID] = client
	h.mu.Unlock()

	if previous != nil && previous != client {
		previous.Close()
	}
	return nil
}

// Serve runs the read loop for a registered client in the current handler
// goroutine and owns the lifetime of its writer goroutine.
func (h *Hub) Serve(client *Client) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		client.Close()
		return
	}
	// Add while holding the same lock used by Close so shutdown cannot observe
	// a serving client between the closed check and the wait-group increment.
	h.wg.Add(1)
	h.mu.Unlock()
	defer h.wg.Done()

	go client.writePump()
	client.readPump(h.logger)
	h.Unregister(client)
	<-client.writerDone
	h.logger.Info("websocket disconnected",
		"user_id", client.UserID,
		"device_id", client.DeviceID,
	)
}

// Unregister removes a client if it is still the active connection for its
// user/device pair, then closes it. The identity check prevents an old
// connection from removing a newer replacement.
func (h *Hub) Unregister(client *Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	if userClients := h.clients[client.UserID]; userClients != nil && userClients[client.DeviceID] == client {
		delete(userClients, client.DeviceID)
		if len(userClients) == 0 {
			delete(h.clients, client.UserID)
		}
	}
	h.mu.Unlock()
	client.Close()
}

// PublishSyncAvailable delivers one coalesced hint to local devices for the
// event's user. The originating device is skipped; it already knows its own
// write completed and will receive remote changes through normal sync rules.
func (h *Hub) PublishSyncAvailable(event SyncAvailableEvent) {
	if err := event.Validate(); err != nil {
		h.logger.Warn("invalid realtime event ignored", "error", err)
		return
	}
	message := NewSyncAvailableMessage(event.LatestCursor)

	h.mu.RLock()
	userClients := h.clients[event.UserID]
	clients := make([]*Client, 0, len(userClients))
	for deviceID, client := range userClients {
		if deviceID != event.OriginDeviceID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range clients {
		if client.enqueue(message) {
			continue
		}
		h.logger.Warn("slow client disconnected",
			"user_id", client.UserID,
			"device_id", client.DeviceID,
		)
		h.Unregister(client)
	}
}

// ConnectionCount returns the number of active local connections. It is useful
// for diagnostics and keeps tests from depending on map internals.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, userClients := range h.clients {
		count += len(userClients)
	}
	return count
}

// Close prevents new registrations, closes all active sockets, and waits for
// every registered connection's read/write loops to exit.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*Client, 0, len(h.clients))
	for _, userClients := range h.clients {
		for _, client := range userClients {
			clients = append(clients, client)
		}
	}
	h.clients = make(map[uuid.UUID]map[string]*Client)
	h.mu.Unlock()

	for _, client := range clients {
		client.Close()
	}
	h.wg.Wait()
}

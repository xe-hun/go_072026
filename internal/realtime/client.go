package realtime

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// A notification is a hint, so eight tiny pending messages is more than
	// enough. Newer sync_available messages replace older pending messages.
	sendQueueSize = 8
	// Clients never send application data. This limit still protects the read
	// loop from arbitrarily large frames used as a resource-exhaustion attack.
	maxIncomingMessageSize = 4 << 10
	writeWait              = 10 * time.Second
	pongWait               = 60 * time.Second
	pingPeriod             = (pongWait * 9) / 10
)

// Client is the minimal local state retained for one authenticated device.
// It contains no user, note, or sync data beyond the identity needed for
// routing notifications.
type Client struct {
	UserID   uuid.UUID
	DeviceID string
	Conn     *websocket.Conn
	Send     chan Message

	done       chan struct{}
	writerDone chan struct{}
	closeOnce  sync.Once
	queueMu    sync.Mutex
	closed     bool
}

// NewClient creates a bounded WebSocket client state object.
func NewClient(userID uuid.UUID, deviceID string, conn *websocket.Conn) *Client {
	return &Client{
		UserID:     userID,
		DeviceID:   deviceID,
		Conn:       conn,
		Send:       make(chan Message, sendQueueSize),
		done:       make(chan struct{}),
		writerDone: make(chan struct{}),
	}
}

// Close interrupts both socket loops. Send is intentionally not closed:
// producers and the writer can race during shutdown, while done provides the
// unambiguous termination signal without send-on-closed-channel panics.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.queueMu.Lock()
		c.closed = true
		c.queueMu.Unlock()
		close(c.done)
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
	})
}

// enqueue adds a message without blocking the hub. When the queue contains
// only sync_available hints, it keeps the highest cursor and drops redundant
// older hints. If another future message type occupies a full queue, false
// tells the hub to disconnect the slow client rather than grow memory.
func (c *Client) enqueue(message Message) bool {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	if c.closed {
		return false
	}
	if message.Type != syncAvailableMessageType {
		select {
		case c.Send <- message:
			return true
		default:
			return false
		}
	}
	if len(c.Send) == 0 {
		select {
		case c.Send <- message:
			return true
		default:
		}
	}

	pending := make([]Message, 0, sendQueueSize)
	highest := message
	allSyncHints := true
	for {
		select {
		case queued := <-c.Send:
			pending = append(pending, queued)
			if queued.Type != syncAvailableMessageType {
				allSyncHints = false
			}
			if queued.Type == syncAvailableMessageType && queued.LatestCursor > highest.LatestCursor {
				highest = queued
			}
		default:
			if !allSyncHints {
				for _, queued := range pending {
					select {
					case c.Send <- queued:
					default:
					}
				}
				return false
			}
			select {
			case c.Send <- highest:
				return true
			default:
				return false
			}
		}
	}
}

// readPump consumes and discards client frames. WebSocket control frames are
// handled by Gorilla; reading is still required for pong heartbeats and for
// detecting disconnects.
func (c *Client) readPump(logger *slog.Logger) {
	if c.Conn == nil {
		return
	}
	c.Conn.SetReadLimit(maxIncomingMessageSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.Conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(
				err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure,
			) {
				logger.Warn("unexpected websocket error",
					"user_id", c.UserID,
					"device_id", c.DeviceID,
					"error", err,
				)
			}
			return
		}
	}
}

// writePump is the only goroutine that writes application or control frames.
// Serializing writes avoids Gorilla's concurrent-write restriction.
func (c *Client) writePump() {
	defer close(c.writerDone)
	if c.Conn == nil {
		return
	}
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.TextMessage, message.wirePayload()); err != nil {
				c.Close()
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.Close()
				return
			}
		}
	}
}

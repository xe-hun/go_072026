package realtime

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"notes-server/internal/auth"
)

func testHub() *Hub {
	return NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testClient(userID uuid.UUID, deviceID string) *Client {
	return NewClient(userID, deviceID, nil)
}

func TestHubRegistersAndRemovesClients(t *testing.T) {
	hub := testHub()
	userID := uuid.New()
	client := testClient(userID, "phone")

	if err := hub.Register(client); err != nil {
		t.Fatal(err)
	}
	if got := hub.ConnectionCount(); got != 1 {
		t.Fatalf("connection count = %d, want 1", got)
	}

	hub.Unregister(client)
	if got := hub.ConnectionCount(); got != 0 {
		t.Fatalf("connection count after unregister = %d, want 0", got)
	}
	client.queueMu.Lock()
	closed := client.closed
	client.queueMu.Unlock()
	if !closed {
		t.Fatal("client was not closed during unregister")
	}
}

func TestHubNotifiesSameUserAndExcludesOrigin(t *testing.T) {
	hub := testHub()
	userID := uuid.New()
	otherUserID := uuid.New()
	origin := testClient(userID, "phone")
	otherDevice := testClient(userID, "tablet")
	unrelated := testClient(otherUserID, "tablet")
	for _, client := range []*Client{origin, otherDevice, unrelated} {
		if err := hub.Register(client); err != nil {
			t.Fatal(err)
		}
	}

	hub.PublishSyncAvailable(SyncAvailableEvent{
		UserID:         userID,
		OriginDeviceID: origin.DeviceID,
		LatestCursor:   8425,
	})

	if len(origin.Send) != 0 {
		t.Fatal("origin device received its own notification")
	}
	if len(unrelated.Send) != 0 {
		t.Fatal("unrelated user received the notification")
	}
	message := <-otherDevice.Send
	if message.Type != syncAvailableMessageType || message.LatestCursor != 8425 {
		t.Fatalf("unexpected notification: %+v", message)
	}
}

func TestHubCoalescesPendingNotifications(t *testing.T) {
	hub := testHub()
	userID := uuid.New()
	client := testClient(userID, "tablet")
	if err := hub.Register(client); err != nil {
		t.Fatal(err)
	}

	for _, cursor := range []int64{8421, 8422, 8423, 8425, 8424} {
		hub.PublishSyncAvailable(SyncAvailableEvent{
			UserID:         userID,
			OriginDeviceID: "phone",
			LatestCursor:   cursor,
		})
	}

	if len(client.Send) != 1 {
		t.Fatalf("pending queue length = %d, want 1", len(client.Send))
	}
	message := <-client.Send
	if message.LatestCursor != 8425 {
		t.Fatalf("coalesced cursor = %d, want 8425", message.LatestCursor)
	}
}

func TestHubDisconnectsClientWhenQueueCannotBeCoalesced(t *testing.T) {
	hub := testHub()
	userID := uuid.New()
	client := testClient(userID, "tablet")
	if err := hub.Register(client); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < sendQueueSize; i++ {
		client.Send <- Message{Type: "future_message"}
	}

	hub.PublishSyncAvailable(SyncAvailableEvent{
		UserID:         userID,
		OriginDeviceID: "phone",
		LatestCursor:   8425,
	})
	if got := hub.ConnectionCount(); got != 0 {
		t.Fatalf("slow client remained registered, connection count = %d", got)
	}
}

func TestSyncAvailableMessagePayload(t *testing.T) {
	message := NewSyncAvailableMessage(8425)
	var payload map[string]any
	if err := json.Unmarshal(message.wirePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "sync_available" || payload["latestCursor"] != float64(8425) {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDecodeSyncAvailableEventRejectsMalformedPayloads(t *testing.T) {
	payloads := []string{"not-json"}
	for _, event := range []SyncAvailableEvent{
		{UserID: uuid.Nil, OriginDeviceID: "phone", LatestCursor: 1},
		{UserID: uuid.New(), OriginDeviceID: "", LatestCursor: 1},
		{UserID: uuid.New(), OriginDeviceID: "phone", LatestCursor: -1},
	} {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, string(raw))
	}
	for _, payload := range payloads {
		if _, err := decodeSyncAvailableEvent(payload); err == nil {
			t.Fatalf("payload %q was accepted", payload)
		}
	}
}

func TestBackoffIsBounded(t *testing.T) {
	delay := initialBackoff
	for i := 0; i < 20; i++ {
		delay = nextBackoff(delay)
	}
	if delay != maxBackoff {
		t.Fatalf("backoff = %s, want %s", delay, maxBackoff)
	}
}

func TestHandlerRejectsUnauthenticatedAndInvalidDevices(t *testing.T) {
	handler := NewHandler(testHub(), nil, slog.Default())

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/v1/realtime", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/realtime?deviceId=", nil)
	request = request.WithContext(auth.WithClaims(request.Context(), auth.Claims{UserID: uuid.New()}))
	invalidDevice := httptest.NewRecorder()
	handler.ServeHTTP(invalidDevice, request)
	if invalidDevice.Code != http.StatusBadRequest {
		t.Fatalf("invalid device status = %d, want %d", invalidDevice.Code, http.StatusBadRequest)
	}
}

func TestHubCloseStopsWebSocketLoops(t *testing.T) {
	serverConnection := make(chan *websocket.Conn, 1)
	releaseServer := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnection <- conn
		<-releaseServer
	}))
	defer server.Close()

	clientConnection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConnection.Close()
	serverConn := <-serverConnection

	hub := testHub()
	client := NewClient(uuid.New(), "phone", serverConn)
	if err := hub.Register(client); err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan struct{})
	go func() {
		hub.Serve(client)
		close(serveDone)
	}()

	deadline := time.Now().Add(time.Second)
	for hub.ConnectionCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	hub.Close()
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("hub close did not stop websocket loops")
	}
	close(releaseServer)
}

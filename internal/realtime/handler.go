package realtime

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

// Handler authenticates and validates a device before upgrading the request.
// Database access happens only during establishment; socket read/write loops
// never query PostgreSQL.
type Handler struct {
	hub      *Hub
	store    *store.Store
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

// NewHandler creates a realtime WebSocket handler using the shared store pool.
func NewHandler(hub *Hub, st *store.Store, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		hub:    hub,
		store:  st,
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// ServeHTTP handles GET /v1/realtime. The authenticated user comes from the
// existing bearer-token middleware; deviceId is supplied as a query parameter
// because WebSocket clients cannot reliably send custom authorization headers
// after the upgrade on every platform.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	deviceID := r.URL.Query().Get(constants.RequestKeyDeviceID)
	if !constants.ValidDeviceID(deviceID) {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyDeviceID+" must be a registered device identifier."))
		return
	}

	device, err := h.store.GetDeviceForOwner(r.Context(), deviceID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.Forbidden("Device does not belong to the authenticated user."))
			return
		}
		httpapi.WriteError(w, r, err)
		return
	}
	if device.RevokedAt.Valid {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked."))
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed",
			"user_id", userID,
			"device_id", deviceID,
			"error", err,
		)
		return
	}
	conn.EnableWriteCompression(false)
	client := NewClient(userID, deviceID, conn)
	if err := h.hub.Register(client); err != nil {
		client.Close()
		h.logger.Warn("websocket registration failed",
			"user_id", userID,
			"device_id", deviceID,
			"error", err,
		)
		return
	}
	h.logger.Info("websocket connected", "user_id", userID, "device_id", deviceID)
	h.hub.Serve(client)
}

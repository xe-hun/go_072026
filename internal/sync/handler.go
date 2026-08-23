package syncapi

import (
	"net/http"
	"strconv"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// Handler is the HTTP adapter for POST and GET /v1/sync and the update
// availability check.
type Handler struct {
	service *Service
}

// PullHTTP parses GET query parameters and returns remote changes.
func (h *Handler) PullHTTP(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	query := r.URL.Query()
	protocolVersion, err := strconv.Atoi(query.Get(constants.RequestKeyProtocolVersion))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyProtocolVersion+" must be an integer."))
		return
	}
	deviceID := query.Get(constants.RequestKeyDeviceID)
	cursor, err := strconv.ParseInt(query.Get(constants.RequestKeyCursor), 10, 64)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyCursor+" must be an integer."))
		return
	}
	resp, err := h.service.Pull(r.Context(), ownerID, PullRequest{
		ProtocolVersion: protocolVersion,
		ClientVersion:   query.Get(constants.RequestKeyClientVersion),
		DeviceID:        deviceID,
		Cursor:          cursor,
	})
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

// UpdatesAvailableHTTP checks whether the authenticated user's device has
// remote changes after its persisted cursor.
func (h *Handler) UpdatesAvailableHTTP(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	query := r.URL.Query()
	protocolVersion, err := strconv.Atoi(query.Get(constants.RequestKeyProtocolVersion))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyProtocolVersion+" must be an integer."))
		return
	}
	resp, err := h.service.UpdatesAvailable(r.Context(), ownerID, UpdatesAvailableRequest{
		ProtocolVersion: protocolVersion,
		DeviceID:        query.Get(constants.RequestKeyDeviceID),
	})
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

// NewHandler wires sync HTTP handling to the sync service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ServeHTTP decodes the sync JSON request, fetches authenticated owner identity,
// delegates to Service, and writes the sync JSON response.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var req Request
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	resp, err := h.service.Sync(r.Context(), ownerID, req)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

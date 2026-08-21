package syncapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// Handler is the HTTP adapter for POST and GET /v1/sync.
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
	protocolVersion, err := strconv.Atoi(query.Get("protocolVersion"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("protocolVersion must be an integer."))
		return
	}
	deviceID, err := uuid.Parse(query.Get("deviceId"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("deviceId must be a UUID."))
		return
	}
	cursor, err := strconv.ParseInt(query.Get("cursor"), 10, 64)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("cursor must be an integer."))
		return
	}
	resp, err := h.service.Pull(r.Context(), ownerID, PullRequest{
		ProtocolVersion: protocolVersion,
		ClientVersion:   query.Get("clientVersion"),
		DeviceID:        deviceID,
		Cursor:          cursor,
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

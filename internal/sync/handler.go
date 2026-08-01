package syncapi

import (
	"net/http"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// Handler is the HTTP adapter for POST /v1/sync.
type Handler struct {
	service *Service
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

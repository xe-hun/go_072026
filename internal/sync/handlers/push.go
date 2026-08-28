package handlers

import (
	"net/http"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	syncapi "notes-server/internal/sync/models"
)

// Push handles POST /v1/sync.
func (h *Handler) Push(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var req syncapi.Request
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

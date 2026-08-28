package handlers

import (
	"net/http"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// List handles GET /v1/devices.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	result, err := h.service.List(r.Context(), ownerID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

package handlers

import (
	"net/http"

	"notes-server/internal/auth"
	"notes-server/internal/devices"
	"notes-server/internal/httpapi"
)

// Register handles POST /v1/devices.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var req devices.RegisterDeviceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	device, err := h.service.Register(r.Context(), ownerID, req)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, device)
}

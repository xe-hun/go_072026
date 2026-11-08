package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// Revoke handles DELETE /v1/devices/{deviceId}.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	deviceID, err := uuid.Parse(chi.URLParam(r, constants.RequestKeyDeviceID))
	if err != nil || deviceID == uuid.Nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyDeviceID+" must be a valid UUID."))
		return
	}
	if err := h.service.Revoke(r.Context(), ownerID, deviceID); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteNoContent(w)
}

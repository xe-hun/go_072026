package handlers

import (
	"net/http"
	"strconv"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	syncapi "notes-server/internal/sync"
)

// UpdatesAvailable handles GET /v1/sync/updates-available.
func (h *Handler) UpdatesAvailable(w http.ResponseWriter, r *http.Request) {
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
	resp, err := h.service.UpdatesAvailable(r.Context(), ownerID, syncapi.UpdatesAvailableRequest{
		ProtocolVersion: protocolVersion,
		DeviceID:        query.Get(constants.RequestKeyDeviceID),
	})
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

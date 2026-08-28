package handlers

import (
	"net/http"
	"strconv"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	syncapi "notes-server/internal/sync"
)

// Pull handles GET /v1/sync.
func (h *Handler) Pull(w http.ResponseWriter, r *http.Request) {
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
	cursor, err := strconv.ParseInt(query.Get(constants.RequestKeyCursor), 10, 64)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyCursor+" must be an integer."))
		return
	}
	resp, err := h.service.Pull(r.Context(), ownerID, syncapi.PullRequest{
		ProtocolVersion: protocolVersion,
		ClientVersion:   query.Get(constants.RequestKeyClientVersion),
		DeviceID:        query.Get(constants.RequestKeyDeviceID),
		Cursor:          cursor,
	})
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

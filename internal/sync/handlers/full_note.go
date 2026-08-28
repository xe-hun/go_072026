package handlers

import (
	"net/http"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	syncapi "notes-server/internal/sync"
)

// FullNote handles POST /v1/syncFullNote.
func (h *Handler) FullNote(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var req syncapi.FullNoteRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	snapshots, err := h.service.SyncFullNote(r.Context(), ownerID, req)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, snapshots)
}

package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// Get handles GET /v1/notes/{noteId}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, constants.RequestKeyNoteID))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("Note ID must be a UUID."))
		return
	}
	note, err := h.service.Get(r.Context(), ownerID, noteID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, note)
}

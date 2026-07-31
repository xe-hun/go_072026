package notes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/{noteId}", h.get)
	r.Get("/{noteId}/snapshot", h.snapshot)
	return r
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, "noteId"))
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

func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, "noteId"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("Note ID must be a UUID."))
		return
	}
	snapshot, err := h.service.LatestSnapshot(r.Context(), ownerID, noteID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, snapshot)
}

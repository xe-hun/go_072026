// Package handlers contains HTTP transport adapters for note reads.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	constants "notes-server/internal"
	"notes-server/internal/notes"
)

type Handler struct {
	service *notes.Service
}

func New(service *notes.Service) *Handler {
	return &Handler{service: service}
}

// Routes returns the note subrouter mounted at /v1/notes.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/{"+constants.RequestKeyNoteID+"}", h.Get)
	r.Get("/{"+constants.RequestKeyNoteID+"}/snapshot", h.LatestSnapshot)
	return r
}

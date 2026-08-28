// Package handlers contains HTTP transport adapters for device management.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	constants "notes-server/internal"
	"notes-server/internal/devices"
)

type Handler struct {
	service *devices.Service
}

func New(service *devices.Service) *Handler {
	return &Handler{service: service}
}

// Routes returns the device subrouter mounted at /v1/devices.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.Register)
	r.Get("/", h.List)
	r.Delete("/{"+constants.RequestKeyDeviceID+"}", h.Revoke)
	return r
}

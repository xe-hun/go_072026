// Package handlers contains the HTTP transport adapters for sync workflows.
package handlers

import syncapi "notes-server/internal/sync"

// Handler delegates HTTP requests to the sync domain service.
type Handler struct {
	service *syncapi.Service
}

// New constructs the sync HTTP handler set.
func New(service *syncapi.Service) *Handler {
	return &Handler{service: service}
}

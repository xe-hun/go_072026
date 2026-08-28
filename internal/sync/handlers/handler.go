// Package handlers contains the HTTP transport adapters for sync workflows.
package handlers

import syncservices "notes-server/internal/sync/services"

// Handler delegates HTTP requests to the sync domain service.
type Handler struct {
	service *syncservices.Service
}

// New constructs the sync HTTP handler set.
func New(service *syncservices.Service) *Handler {
	return &Handler{service: service}
}

package devices

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
	r.Post("/", h.register)
	r.Get("/", h.list)
	r.Delete("/{deviceId}", h.revoke)
	return r
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var req RegisterDeviceRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	device, err := h.service.Register(r.Context(), ownerID, req)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, device)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	devices, err := h.service.List(r.Context(), ownerID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, devices)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	deviceID, err := uuid.Parse(chi.URLParam(r, "deviceId"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("Device ID must be a UUID."))
		return
	}
	if err := h.service.Revoke(r.Context(), ownerID, deviceID); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteNoContent(w)
}

// Package handlers contains HTTP adapters for private note file uploads.
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	constants "notes-server/internal"
	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
	"notes-server/internal/uploads"
)

type Handler struct {
	service *uploads.Service
}

func New(service *uploads.Service) *Handler {
	return &Handler{service: service}
}

// Routes returns the upload subrouter mounted at /v1/uploads.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/initiate", h.Initiate)
	r.Get("/download-links", h.DownloadLinks)
	r.Get("/{"+constants.RequestKeyUploadID+"}", h.Get)
	r.Post("/{"+constants.RequestKeyUploadID+"}/complete", h.Complete)
	r.Post("/{"+constants.RequestKeyBlockId+"}/download-url", h.DownloadURL)
	r.Delete("/{"+constants.RequestKeyUploadID+"}", h.Delete)
	return r
}

func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	var request uploads.InitiateRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	response, err := h.service.Initiate(r.Context(), ownerID, request)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, response)
}

// DownloadLinks handles GET /v1/uploads/download-links?cursor=...&deviceId=....
func (h *Handler) DownloadLinks(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return
	}
	query := r.URL.Query()
	cursor, err := strconv.ParseInt(query.Get(constants.RequestKeyCursor), 10, 64)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyCursor+" must be an integer."))
		return
	}
	deviceID, err := uuid.Parse(query.Get(constants.RequestKeyDeviceID))
	if err != nil || deviceID == uuid.Nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest(constants.RequestKeyDeviceID+" must be a valid UUID."))
		return
	}
	response, err := h.service.DownloadURLs(r.Context(), ownerID, deviceID, cursor)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID, uploadID, ok := h.identity(w, r)
	if !ok {
		return
	}
	response, err := h.service.Get(r.Context(), ownerID, uploadID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	ownerID, uploadID, ok := h.identity(w, r)
	if !ok {
		return
	}
	response, err := h.service.Complete(r.Context(), ownerID, uploadID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) DownloadURL(w http.ResponseWriter, r *http.Request) {
	ownerID, blockID, ok := h.blockIdentity(w, r)
	if !ok {
		return
	}
	response, err := h.service.DownloadURL(r.Context(), ownerID, blockID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) blockIdentity(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return uuid.Nil, uuid.Nil, false
	}
	blockID, err := uuid.Parse(chi.URLParam(r, constants.RequestKeyBlockId))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("Block ID must be a UUID."))
		return uuid.Nil, uuid.Nil, false
	}
	return ownerID, blockID, true
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID, uploadID, ok := h.identity(w, r)
	if !ok {
		return
	}
	_, err := h.service.Delete(r.Context(), ownerID, uploadID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	httpapi.WriteNoContent(w)
}

func (h *Handler) identity(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	ownerID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.Unauthorized())
		return uuid.Nil, uuid.Nil, false
	}
	uploadID, err := uuid.Parse(chi.URLParam(r, constants.RequestKeyUploadID))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.InvalidRequest("Upload ID must be a UUID."))
		return uuid.Nil, uuid.Nil, false
	}
	return ownerID, uploadID, true
}

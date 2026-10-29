package uploads

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	constants "notes-server/internal"
	"notes-server/internal/config"
	"notes-server/internal/httpapi"
	"notes-server/internal/storage"
	"notes-server/internal/store"
)

// Repository is the upload persistence surface used by the service.
type Repository interface {
	ValidateUploadTarget(ctx context.Context, noteID, blockID, ownerID uuid.UUID) error
	GetDeviceForOwner(ctx context.Context, deviceID string, ownerID uuid.UUID) (store.SyncDevice, error)
	CountPendingFileUploads(ctx context.Context, ownerID uuid.UUID) (int64, error)
	CreateFileUpload(ctx context.Context, upload store.FileUpload) (store.FileUpload, error)
	GetFileUploadForOwner(ctx context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error)
	ListFileUploadsAfterCursor(ctx context.Context, ownerID uuid.UUID, cursor int64) ([]store.FileUpload, error)
	MarkFileUploadFailed(ctx context.Context, uploadID, ownerID uuid.UUID, message string) (store.FileUpload, error)
	CompleteFileUpload(ctx context.Context, uploadID, ownerID uuid.UUID, actualSize int64, verifiedContentType string) (store.FileUpload, error)
	MarkFileUploadCancelled(ctx context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error)
	MarkFileUploadDeleted(ctx context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error)
}

// Service owns authorization, metadata validation, upload lifecycle rules, and
// the database/storage consistency decisions. It never accepts owner IDs from a
// request body.
type Service struct {
	repository       Repository
	storage          storage.Storage
	bucket           string
	maxFileSize      int64
	allowedMIMETypes []string
	uploadTTL        time.Duration
	downloadTTL      time.Duration
	maxPending       int
}

// NewService wires upload rules to the existing store and trusted Storage
// client.
func NewService(repository Repository, storageClient storage.Storage, cfg config.Config) *Service {
	return &Service{
		repository:       repository,
		storage:          storageClient,
		bucket:           cfg.SupabaseStorageBucket,
		maxFileSize:      cfg.SupabaseStorageMaxFileSize,
		allowedMIMETypes: cfg.SupabaseStorageAllowedMimeTypes,
		uploadTTL:        cfg.SupabaseStorageUploadTTL,
		downloadTTL:      cfg.SupabaseStorageDownloadTTL,
		maxPending:       cfg.SupabaseStorageMaxPendingUploads,
	}
}

// Initiate validates the target and metadata, creates a durable pending row,
// then requests a signed authorization. The row is created first so an
// authorization is never returned for an unidentifiable upload.
func (s *Service) Initiate(ctx context.Context, ownerID uuid.UUID, request InitiateRequest) (InitiateResponse, error) {
	if err := validateRequest(request, s.maxFileSize, s.allowedMIMETypes); err != nil {
		return InitiateResponse{}, err
	}
	if err := s.validateDevice(ctx, ownerID, request.DeviceID); err != nil {
		return InitiateResponse{}, err
	}
	if err := s.repository.ValidateUploadTarget(ctx, request.NoteID, request.BlockID, ownerID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return InitiateResponse{}, httpapi.NotFound("Note or block not found.")
		}
		return InitiateResponse{}, err
	}
	pendingCount, err := s.repository.CountPendingFileUploads(ctx, ownerID)
	if err != nil {
		return InitiateResponse{}, err
	}
	if pendingCount >= int64(s.maxPending) {
		return InitiateResponse{}, httpapi.NewError(http.StatusTooManyRequests, httpapi.CodeRateLimited, "Too many pending uploads. Complete or delete an existing upload first.")
	}

	uploadID := uuid.New()
	contentType := normalizedContentType(request.ContentType)
	path := fmt.Sprintf("notes/%s/blocks/%s/uploads/%s.%s", request.NoteID, request.BlockID, uploadID, extensionForContentType(contentType))
	expiresAt := time.Now().UTC().Add(s.uploadTTL)
	entity, err := s.repository.CreateFileUpload(ctx, store.FileUpload{
		ID:               uploadID,
		NoteID:           request.NoteID,
		OwnerID:          ownerID,
		BlockID:          request.BlockID,
		DeviceID:         request.DeviceID,
		OriginalFilename: strings.TrimSpace(request.Filename),
		StorageBucket:    s.bucket,
		StoragePath:      path,
		ContentType:      contentType,
		ExpectedSize:     request.Size,
		Status:           StatusPending,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		return InitiateResponse{}, err
	}

	signed, err := s.storage.CreateSignedUploadURL(ctx, entity.StorageBucket, entity.StoragePath)
	if err != nil {
		// The path is unique and no object exists yet. Retaining a FAILED row
		// makes the failed authorization visible to cleanup/reconciliation.
		_, _ = s.repository.MarkFileUploadFailed(ctx, entity.ID, ownerID, "storage authorization failed")
		return InitiateResponse{}, storageAPIError(err)
	}
	return InitiateResponse{
		UploadID:    entity.ID,
		Cursor:      entity.Cursor,
		Status:      StatusPending,
		Bucket:      entity.StorageBucket,
		Path:        entity.StoragePath,
		ContentType: entity.ContentType,
		SignedURL:   signed.URL,
		Token:       signed.Token,
		ExpiresAt:   entity.ExpiresAt,
	}, nil
}

// Complete independently verifies Storage metadata before the conditional
// database update. Concurrent completion requests are safe: only one pending
// row can transition through CompleteFileUpload, and later retries return the
// already completed row.
func (s *Service) Complete(ctx context.Context, ownerID, uploadID uuid.UUID) (Response, error) {
	entity, err := s.repository.GetFileUploadForOwner(ctx, uploadID, ownerID)
	if err != nil {
		return Response{}, mapRepositoryError(err, "Upload not found.")
	}
	if entity.Status == StatusCompleted {
		return responseFromEntity(entity), nil
	}
	if entity.Status != StatusPending {
		return Response{}, httpapi.NewError(http.StatusConflict, httpapi.CodeConflict, "Upload is no longer pending.")
	}
	if time.Now().UTC().After(entity.ExpiresAt) {
		// Leave the row pending until the worker's grace-period reconciliation.
		// An object may have arrived just after the nominal authorization expiry;
		// marking it expired here would prevent the worker from inspecting and
		// safely cleaning that late object.
		return Response{}, httpapi.NewError(http.StatusConflict, httpapi.CodeUploadNotReady, "The upload authorization has expired.")
	}
	if err := s.repository.ValidateUploadTarget(ctx, entity.NoteID, entity.BlockID, ownerID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Response{}, httpapi.Forbidden("You no longer have access to this note or block.")
		}
		return Response{}, err
	}

	info, err := s.storage.GetObjectInfo(ctx, entity.StorageBucket, entity.StoragePath)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return Response{}, httpapi.NewError(http.StatusConflict, httpapi.CodeUploadNotReady, "The uploaded object is not ready yet.")
		}
		return Response{}, storageAPIError(err)
	}
	if info.Size != entity.ExpectedSize {
		return Response{}, s.rejectMetadata(ctx, entity, "The stored file size does not match the declared size.")
	}
	verifiedContentType := normalizedContentType(info.ContentType)
	if verifiedContentType == "" || verifiedContentType != entity.ContentType {
		return Response{}, s.rejectMetadata(ctx, entity, "The stored file type does not match the declared type.")
	}

	completed, err := s.repository.CompleteFileUpload(ctx, entity.ID, ownerID, info.Size, verifiedContentType)
	if err == nil {
		return responseFromEntity(completed), nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Response{}, err
	}
	// Another request may have won the conditional update while this request
	// was checking Storage. Re-read and return that durable result.
	latest, readErr := s.repository.GetFileUploadForOwner(ctx, entity.ID, ownerID)
	if readErr != nil {
		return Response{}, mapRepositoryError(readErr, "Upload not found.")
	}
	if latest.Status == StatusCompleted {
		return responseFromEntity(latest), nil
	}
	return Response{}, httpapi.NewError(http.StatusConflict, httpapi.CodeConflict, "Upload completion is already being finalized.")
}

// Get returns public lifecycle metadata without exposing Storage credentials.
func (s *Service) Get(ctx context.Context, ownerID, uploadID uuid.UUID) (Response, error) {
	entity, err := s.repository.GetFileUploadForOwner(ctx, uploadID, ownerID)
	if err != nil {
		return Response{}, mapRepositoryError(err, "Upload not found.")
	}
	return responseFromEntity(entity), nil
}

// DownloadURL verifies ownership and completion before creating a short-lived
// private-bucket URL.
func (s *Service) DownloadURL(ctx context.Context, ownerID, uploadID uuid.UUID) (DownloadURLResponse, error) {
	entity, err := s.repository.GetFileUploadForOwner(ctx, uploadID, ownerID)
	if err != nil {
		return DownloadURLResponse{}, mapRepositoryError(err, "Upload not found.")
	}
	if entity.Status != StatusCompleted {
		return DownloadURLResponse{}, httpapi.NewError(http.StatusConflict, httpapi.CodeConflict, "Only completed uploads can be downloaded.")
	}
	if err := s.repository.ValidateUploadTarget(ctx, entity.NoteID, entity.BlockID, ownerID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DownloadURLResponse{}, httpapi.Forbidden("You do not have access to this note or block.")
		}
		return DownloadURLResponse{}, err
	}
	urlValue, err := s.storage.CreateSignedDownloadURL(ctx, entity.StorageBucket, entity.StoragePath, int(s.downloadTTL/time.Second))
	if err != nil {
		return DownloadURLResponse{}, storageAPIError(err)
	}
	return DownloadURLResponse{
		Cursor:    entity.Cursor,
		UploadID:  entity.ID,
		SignedURL: urlValue,
		ExpiresAt: time.Now().UTC().Add(s.downloadTTL),
	}, nil
}

// DownloadURLs returns completed upload links after cursor in feed order. Rows
// from the requesting device are consumed but never returned. A pending row
// from another device stops cursor advancement so a later completion cannot be
// skipped by the client.
func (s *Service) DownloadURLs(ctx context.Context, ownerID uuid.UUID, deviceID string, cursor int64) (DownloadLinksResponse, error) {
	if err := validateCursor(cursor); err != nil {
		return DownloadLinksResponse{}, err
	}
	if err := s.validateDevice(ctx, ownerID, deviceID); err != nil {
		return DownloadLinksResponse{}, err
	}
	entities, err := s.repository.ListFileUploadsAfterCursor(ctx, ownerID, cursor)
	if err != nil {
		return DownloadLinksResponse{}, err
	}
	response := DownloadLinksResponse{
		Links:      make([]DownloadURLResponse, 0, len(entities)),
		NextCursor: cursor,
	}
	for _, entity := range entities {
		if entity.DeviceID == deviceID {
			response.NextCursor = entity.Cursor
			continue
		}
		if entity.Status == StatusPending {
			break
		}
		if entity.Status != StatusCompleted {
			response.NextCursor = entity.Cursor
			continue
		}
		if err := s.repository.ValidateUploadTarget(ctx, entity.NoteID, entity.BlockID, ownerID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return DownloadLinksResponse{}, httpapi.Forbidden("You do not have access to this note or block.")
			}
			return DownloadLinksResponse{}, err
		}
		urlValue, err := s.storage.CreateSignedDownloadURL(ctx, entity.StorageBucket, entity.StoragePath, int(s.downloadTTL/time.Second))
		if err != nil {
			return DownloadLinksResponse{}, storageAPIError(err)
		}
		response.Links = append(response.Links, DownloadURLResponse{
			Cursor:    entity.Cursor,
			UploadID:  entity.ID,
			SignedURL: urlValue,
			ExpiresAt: time.Now().UTC().Add(s.downloadTTL),
		})
		response.NextCursor = entity.Cursor
	}
	return response, nil
}

// Delete removes the object first, then records DELETED. Repeating the request
// is safe because a missing object is already the desired storage state and the
// conditional database update prevents a second lifecycle transition.
func (s *Service) Delete(ctx context.Context, ownerID, uploadID uuid.UUID) (Response, error) {
	entity, err := s.repository.GetFileUploadForOwner(ctx, uploadID, ownerID)
	if err != nil {
		return Response{}, mapRepositoryError(err, "Upload not found.")
	}
	if entity.Status == StatusDeleted {
		return responseFromEntity(entity), nil
	}
	// Reserve the row first so a concurrent completion cannot mark an object
	// completed after this deletion has removed it from Storage. CANCELLED is
	// also a retryable deletion-intent state if Storage is temporarily down.
	entity, err = s.repository.MarkFileUploadCancelled(ctx, entity.ID, ownerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			latest, readErr := s.repository.GetFileUploadForOwner(ctx, entity.ID, ownerID)
			if readErr == nil && latest.Status == StatusDeleted {
				return responseFromEntity(latest), nil
			}
		}
		return Response{}, err
	}
	if err := s.storage.DeleteObject(ctx, entity.StorageBucket, entity.StoragePath); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Response{}, storageAPIError(err)
	}
	deleted, err := s.repository.MarkFileUploadDeleted(ctx, entity.ID, ownerID)
	if err == nil {
		return responseFromEntity(deleted), nil
	}
	if errors.Is(err, store.ErrNotFound) {
		latest, readErr := s.repository.GetFileUploadForOwner(ctx, entity.ID, ownerID)
		if readErr == nil && latest.Status == StatusDeleted {
			return responseFromEntity(latest), nil
		}
	}
	return Response{}, err
}

func (s *Service) rejectMetadata(ctx context.Context, entity store.FileUpload, message string) error {
	_, _ = s.repository.MarkFileUploadFailed(ctx, entity.ID, entity.OwnerID, strings.ToLower(strings.TrimSuffix(message, ".")))
	return httpapi.NewError(http.StatusUnprocessableEntity, httpapi.CodeUploadInvalid, message)
}

func validateRequest(request InitiateRequest, maxFileSize int64, allowedMIMETypes []string) error {
	if err := validateDeviceID(request.DeviceID); err != nil {
		return err
	}
	if request.NoteID == uuid.Nil || request.BlockID == uuid.Nil {
		return httpapi.InvalidRequest("Note ID and block ID are required.")
	}
	if request.Size <= 0 {
		return httpapi.InvalidRequest("File size must be greater than zero.")
	}
	if request.Size > maxFileSize {
		return httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The file exceeds the configured size limit.")
	}
	filename := strings.TrimSpace(request.Filename)
	if filename == "" || len(filename) > 255 || !utf8.ValidString(filename) || strings.ContainsAny(filename, "/\\") {
		return httpapi.InvalidRequest("Filename is invalid.")
	}
	for _, runeValue := range filename {
		if unicode.IsControl(runeValue) {
			return httpapi.InvalidRequest("Filename is invalid.")
		}
	}
	contentType := normalizedContentType(request.ContentType)
	if contentType == "" || !mimeAllowed(contentType, allowedMIMETypes) {
		return httpapi.InvalidRequest("File type is not allowed.")
	}
	return nil
}

func validateCursor(cursor int64) error {
	if cursor < 0 {
		return httpapi.InvalidRequest(constants.RequestKeyCursor + " must be greater than or equal to zero.")
	}
	return nil
}

func validateDeviceID(deviceID string) error {
	if deviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(deviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	return nil
}

func (s *Service) validateDevice(ctx context.Context, ownerID uuid.UUID, deviceID string) error {
	if err := validateDeviceID(deviceID); err != nil {
		return err
	}
	device, err := s.repository.GetDeviceForOwner(ctx, deviceID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return httpapi.Forbidden("Device does not belong to the authenticated user.")
	}
	if err != nil {
		return err
	}
	if device.RevokedAt.Valid {
		return httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked.")
	}
	return nil
}

func normalizedContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func mimeAllowed(contentType string, allowed []string) bool {
	for _, value := range allowed {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == contentType || strings.HasSuffix(value, "/*") && strings.HasPrefix(contentType, strings.TrimSuffix(value, "*")) {
			return true
		}
	}
	return false
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "application/pdf":
		return "pdf"
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	}
	if extensions, err := mime.ExtensionsByType(contentType); err == nil && len(extensions) > 0 {
		return strings.TrimPrefix(extensions[0], ".")
	}
	return "bin"
}

func mapRepositoryError(err error, notFoundMessage string) error {
	if errors.Is(err, store.ErrNotFound) {
		return httpapi.NotFound(notFoundMessage)
	}
	return err
}

func storageAPIError(err error) error {
	switch {
	case errors.Is(err, storage.ErrUnavailable):
		return httpapi.NewError(http.StatusServiceUnavailable, httpapi.CodeStorageUnavailable, "File storage is temporarily unavailable.")
	case errors.Is(err, storage.ErrNotFound):
		return httpapi.NotFound("Stored file not found.")
	case errors.Is(err, storage.ErrUpstream), errors.Is(err, storage.ErrInvalidResponse):
		return httpapi.NewError(http.StatusBadGateway, httpapi.CodeStorageUnavailable, "File storage returned an invalid response.")
	default:
		return err
	}
}

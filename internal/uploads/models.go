package uploads

import (
	"time"

	"github.com/google/uuid"

	"notes-server/internal/store"
)

const (
	StatusPending   = "PENDING"
	StatusCompleted = "COMPLETED"
	StatusFailed    = "FAILED"
	StatusExpired   = "EXPIRED"
	StatusCancelled = "CANCELLED"
	StatusDeleted   = "DELETED"
)

// InitiateRequest is the client-supplied file metadata. It intentionally has
// no user ID; ownership always comes from the authenticated request context.
type InitiateRequest struct {
	NoteID      uuid.UUID `json:"noteId"`
	BlockID     uuid.UUID `json:"blockId"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
}

// InitiateResponse contains the only response that includes a signed upload
// capability. The token is never stored in PostgreSQL.
type InitiateResponse struct {
	UploadID    uuid.UUID `json:"uploadId"`
	Status      string    `json:"status"`
	Bucket      string    `json:"bucket"`
	Path        string    `json:"path"`
	ContentType string    `json:"contentType"`
	SignedURL   string    `json:"signedUrl"`
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// FileMetadata is the public file description returned after verification.
type FileMetadata struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// Response is safe for status, completion, and deletion responses. It contains
// no upload token, signed URL, secret, or internal error detail.
type Response struct {
	UploadID            uuid.UUID    `json:"uploadId"`
	NoteID              uuid.UUID    `json:"noteId"`
	BlockID             uuid.UUID    `json:"blockId"`
	Status              string       `json:"status"`
	File                FileMetadata `json:"file"`
	ExpectedSize        int64        `json:"expectedSize"`
	ActualSize          *int64       `json:"actualSize,omitempty"`
	VerifiedContentType string       `json:"verifiedContentType,omitempty"`
	Bucket              string       `json:"bucket"`
	Path                string       `json:"path"`
	ExpiresAt           time.Time    `json:"expiresAt"`
	CompletedAt         *time.Time   `json:"completedAt,omitempty"`
	CreatedAt           time.Time    `json:"createdAt"`
	UpdatedAt           time.Time    `json:"updatedAt"`
}

// DownloadURLResponse contains a short-lived bearer capability and no storage
// credentials.
type DownloadURLResponse struct {
	UploadID  uuid.UUID `json:"uploadId"`
	SignedURL string    `json:"signedUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func responseFromEntity(upload store.FileUpload) Response {
	response := Response{
		UploadID:     upload.ID,
		NoteID:       upload.NoteID,
		BlockID:      upload.BlockID,
		Status:       upload.Status,
		ExpectedSize: upload.ExpectedSize,
		Bucket:       upload.StorageBucket,
		Path:         upload.StoragePath,
		ExpiresAt:    upload.ExpiresAt,
		CreatedAt:    upload.CreatedAt,
		UpdatedAt:    upload.UpdatedAt,
		File: FileMetadata{
			Filename:    upload.OriginalFilename,
			ContentType: upload.ContentType,
			Size:        upload.ExpectedSize,
		},
	}
	if upload.ActualSize.Valid {
		actualSize := upload.ActualSize.Int64
		response.ActualSize = &actualSize
		response.File.Size = actualSize
	}
	if upload.VerifiedContentType.Valid {
		response.VerifiedContentType = upload.VerifiedContentType.String
		response.File.ContentType = upload.VerifiedContentType.String
	}
	if upload.CompletedAt.Valid {
		completedAt := upload.CompletedAt.Time
		response.CompletedAt = &completedAt
	}
	return response
}

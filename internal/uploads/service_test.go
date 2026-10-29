package uploads

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"notes-server/internal/config"
	"notes-server/internal/storage"
	"notes-server/internal/store"
)

type fakeRepository struct {
	mu      sync.Mutex
	uploads map[uuid.UUID]store.FileUpload
	cursor  int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{uploads: make(map[uuid.UUID]store.FileUpload)}
}

func (r *fakeRepository) ValidateUploadTarget(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

func (r *fakeRepository) GetDeviceForOwner(_ context.Context, deviceID string, ownerID uuid.UUID) (store.SyncDevice, error) {
	return store.SyncDevice{ID: deviceID, OwnerID: ownerID}, nil
}

func (r *fakeRepository) CountPendingFileUploads(_ context.Context, ownerID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, upload := range r.uploads {
		if upload.OwnerID == ownerID && upload.Status == StatusPending && upload.ExpiresAt.After(time.Now()) {
			count++
		}
	}
	return count, nil
}

func (r *fakeRepository) CreateFileUpload(_ context.Context, upload store.FileUpload) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursor++
	upload.Cursor = r.cursor
	upload.Status = StatusPending
	upload.CreatedAt = time.Now().UTC()
	upload.UpdatedAt = upload.CreatedAt
	r.uploads[upload.ID] = upload
	return upload, nil
}

func (r *fakeRepository) ListFileUploadsAfterCursor(_ context.Context, ownerID uuid.UUID, cursor int64) ([]store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]store.FileUpload, 0)
	for _, upload := range r.uploads {
		if upload.OwnerID == ownerID && upload.Cursor > cursor {
			result = append(result, upload)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Cursor < result[j].Cursor })
	return result, nil
}

func (r *fakeRepository) GetFileUploadForOwner(_ context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[uploadID]
	if !ok || upload.OwnerID != ownerID {
		return store.FileUpload{}, store.ErrNotFound
	}
	return upload, nil
}

func (r *fakeRepository) MarkFileUploadFailed(_ context.Context, uploadID, ownerID uuid.UUID, message string) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[uploadID]
	if !ok || upload.OwnerID != ownerID {
		return store.FileUpload{}, store.ErrNotFound
	}
	upload.Status = StatusFailed
	upload.ErrorMessage.String = message
	upload.ErrorMessage.Valid = true
	r.uploads[uploadID] = upload
	return upload, nil
}

func (r *fakeRepository) CompleteFileUpload(_ context.Context, uploadID, ownerID uuid.UUID, size int64, contentType string) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[uploadID]
	if !ok || upload.OwnerID != ownerID || upload.Status != StatusPending {
		return store.FileUpload{}, store.ErrNotFound
	}
	upload.Status = StatusCompleted
	upload.ActualSize.Int64 = size
	upload.ActualSize.Valid = true
	upload.VerifiedContentType.String = contentType
	upload.VerifiedContentType.Valid = true
	now := time.Now().UTC()
	upload.CompletedAt.Time = now
	upload.CompletedAt.Valid = true
	upload.UpdatedAt = now
	r.uploads[uploadID] = upload
	return upload, nil
}

func (r *fakeRepository) MarkFileUploadCancelled(_ context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[uploadID]
	if !ok || upload.OwnerID != ownerID || upload.Status == StatusDeleted {
		return store.FileUpload{}, store.ErrNotFound
	}
	upload.Status = StatusCancelled
	r.uploads[uploadID] = upload
	return upload, nil
}

func (r *fakeRepository) MarkFileUploadDeleted(_ context.Context, uploadID, ownerID uuid.UUID) (store.FileUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[uploadID]
	if !ok || upload.OwnerID != ownerID {
		return store.FileUpload{}, store.ErrNotFound
	}
	upload.Status = StatusDeleted
	r.uploads[uploadID] = upload
	return upload, nil
}

type fakeStorage struct {
	signed      storage.SignedUpload
	info        storage.ObjectInfo
	infoErr     error
	infoCalls   int
	deleteCalls int
	createErr   error
	deleteErr   error
}

func (s *fakeStorage) CreateSignedUploadURL(context.Context, string, string) (storage.SignedUpload, error) {
	return s.signed, s.createErr
}

func (s *fakeStorage) GetObjectInfo(context.Context, string, string) (storage.ObjectInfo, error) {
	s.infoCalls++
	return s.info, s.infoErr
}

func (s *fakeStorage) CreateSignedDownloadURL(context.Context, string, string, int) (string, error) {
	return "https://example.test/download?token=download", nil
}

func (s *fakeStorage) DeleteObject(context.Context, string, string) error {
	s.deleteCalls++
	return s.deleteErr
}

func testConfig() config.Config {
	return config.Config{
		SupabaseStorageBucket:            "documents",
		SupabaseStorageMaxFileSize:       10 * 1024 * 1024,
		SupabaseStorageAllowedMimeTypes:  []string{"application/pdf"},
		SupabaseStorageMaxPendingUploads: 20,
		SupabaseStorageUploadTTL:         2 * time.Hour,
		SupabaseStorageDownloadTTL:       time.Minute,
	}
}

func TestInitiateCreatesPendingRecordAndUniquePath(t *testing.T) {
	repository := newFakeRepository()
	storageClient := &fakeStorage{signed: storage.SignedUpload{URL: "https://storage.test/upload", Token: "secret-token"}}
	service := NewService(repository, storageClient, testConfig())
	ownerID := uuid.New()
	request := InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "phone", Filename: "document.pdf", ContentType: "application/pdf", Size: 12}
	response, err := service.Initiate(context.Background(), ownerID, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusPending || response.Token != "secret-token" || response.Path == "" {
		t.Fatalf("unexpected initiation response: %+v", response)
	}
	upload, err := repository.GetFileUploadForOwner(context.Background(), response.UploadID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if upload.Status != StatusPending || upload.OwnerID != ownerID || upload.StoragePath != response.Path {
		t.Fatalf("unexpected pending record: %+v", upload)
	}
}

func TestCompleteVerifiesMetadataAndIsIdempotent(t *testing.T) {
	repository := newFakeRepository()
	storageClient := &fakeStorage{
		signed: storage.SignedUpload{URL: "https://storage.test/upload", Token: "secret-token"},
		info:   storage.ObjectInfo{Size: 12, ContentType: "application/pdf"},
	}
	service := NewService(repository, storageClient, testConfig())
	ownerID := uuid.New()
	initResponse, err := service.Initiate(context.Background(), ownerID, InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "phone", Filename: "document.pdf", ContentType: "application/pdf", Size: 12})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Complete(context.Background(), ownerID, initResponse.UploadID)
	if err != nil || first.Status != StatusCompleted || first.ActualSize == nil || *first.ActualSize != 12 {
		t.Fatalf("unexpected completion: %+v, %v", first, err)
	}
	second, err := service.Complete(context.Background(), ownerID, initResponse.UploadID)
	if err != nil || second.Status != StatusCompleted || storageClient.infoCalls != 1 {
		t.Fatalf("completion retry was not idempotent: %+v, %v, info calls=%d", second, err, storageClient.infoCalls)
	}
}

func TestCompleteRejectsMetadataMismatch(t *testing.T) {
	repository := newFakeRepository()
	storageClient := &fakeStorage{info: storage.ObjectInfo{Size: 99, ContentType: "application/pdf"}}
	service := NewService(repository, storageClient, testConfig())
	ownerID := uuid.New()
	initResponse, err := service.Initiate(context.Background(), ownerID, InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "phone", Filename: "document.pdf", ContentType: "application/pdf", Size: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), ownerID, initResponse.UploadID); err == nil {
		t.Fatal("expected metadata mismatch to fail")
	}
	upload, err := repository.GetFileUploadForOwner(context.Background(), initResponse.UploadID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if upload.Status != StatusFailed {
		t.Fatalf("mismatched upload status = %s, want FAILED", upload.Status)
	}
}

func TestInitiateRejectsUnsafeOrUnsupportedMetadata(t *testing.T) {
	repository := newFakeRepository()
	service := NewService(repository, &fakeStorage{}, testConfig())
	base := InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "phone", Filename: "document.pdf", ContentType: "application/pdf", Size: 12}
	invalid := []InitiateRequest{
		{NoteID: base.NoteID, BlockID: base.BlockID, Filename: "document.pdf", ContentType: "application/pdf", Size: 0},
		{NoteID: base.NoteID, BlockID: base.BlockID, Filename: "document.pdf", ContentType: "application/pdf", Size: 11 * 1024 * 1024},
		{NoteID: base.NoteID, BlockID: base.BlockID, Filename: "document.exe", ContentType: "application/octet-stream", Size: 12},
		{NoteID: base.NoteID, BlockID: base.BlockID, Filename: "../document.pdf", ContentType: "application/pdf", Size: 12},
	}
	for _, request := range invalid {
		if _, err := service.Initiate(context.Background(), uuid.New(), request); err == nil {
			t.Fatalf("expected invalid metadata to be rejected: %+v", request)
		}
	}
}

func TestDownloadURLsReturnsOnlyCompletedUploadsFromOtherDevices(t *testing.T) {
	repository := newFakeRepository()
	storageClient := &fakeStorage{
		signed: storage.SignedUpload{URL: "https://storage.test/upload", Token: "secret-token"},
		info:   storage.ObjectInfo{Size: 12, ContentType: "application/pdf"},
	}
	service := NewService(repository, storageClient, testConfig())
	ownerID := uuid.New()
	phoneUpload, err := service.Initiate(context.Background(), ownerID, InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "phone", Filename: "phone.pdf", ContentType: "application/pdf", Size: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), ownerID, phoneUpload.UploadID); err != nil {
		t.Fatal(err)
	}
	laptopUpload, err := service.Initiate(context.Background(), ownerID, InitiateRequest{NoteID: uuid.New(), BlockID: uuid.New(), DeviceID: "laptop", Filename: "laptop.pdf", ContentType: "application/pdf", Size: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), ownerID, laptopUpload.UploadID); err != nil {
		t.Fatal(err)
	}
	response, err := service.DownloadURLs(context.Background(), ownerID, "phone", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Links) != 1 || response.Links[0].UploadID != laptopUpload.UploadID || response.NextCursor != laptopUpload.Cursor {
		t.Fatalf("unexpected download feed response: %+v", response)
	}
}

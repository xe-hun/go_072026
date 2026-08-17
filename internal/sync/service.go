package syncapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"notes-server/internal/config"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

var errOperationBatchRejected = errors.New("operation batch rejected")

// Service owns sync business rules. It is deliberately above the store layer so
// transactions can include both current-state writes and change-log inserts.
type Service struct {
	// store is the persistence boundary.
	store *store.Store
	// logger records aggregate sync outcomes without logging note content.
	logger *slog.Logger
	// maxOperations bounds batch size.
	maxOperations int
	// defaultPullLimit is used when clients omit limit.
	defaultPullLimit int32
	// maxPullLimit caps response size.
	maxPullLimit int32
	// snapshotChangeThreshold controls snapshot job enqueueing by count.
	snapshotChangeThreshold int64
	// snapshotByteThreshold controls snapshot job enqueueing by payload bytes.
	snapshotByteThreshold int64
}

// NewService builds a sync service using validated runtime limits.
func NewService(store *store.Store, cfg config.Config, logger *slog.Logger) *Service {
	return &Service{
		store:                   store,
		logger:                  logger,
		maxOperations:           cfg.SyncMaxOperations,
		defaultPullLimit:        int32(cfg.SyncDefaultPullLimit),
		maxPullLimit:            int32(cfg.SyncMaxPullLimit),
		snapshotChangeThreshold: cfg.SnapshotChangeThreshold,
		snapshotByteThreshold:   cfg.SnapshotByteThreshold,
	}
}

// Sync applies client operations and pulls remote changes in one transaction.
// A valid request can contain accepted and rejected operations; only
// request-level failures return an HTTP error.
func (s *Service) Sync(ctx context.Context, ownerID uuid.UUID, req Request) (Response, error) {
	if err := req.Validate(s.maxOperations); err != nil {
		return Response{}, err
	}

	limit, err := req.PullLimit(s.defaultPullLimit, s.maxPullLimit)
	if err != nil {
		return Response{}, err
	}

	resp := Response{
		Accepted:   []AcceptedDTO{},
		Rejected:   []RejectedDTO{},
		Changes:    []PulledChange{},
		NextCursor: req.Cursor,
	}

	err = s.store.WithTx(ctx, func(tx *store.Store) error {
		// Lock the device row so cursor updates for the same device serialize.
		device, err := tx.GetDeviceForOwnerForUpdate(ctx, req.DeviceID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			return httpapi.Forbidden("Device does not belong to the authenticated user.")
		}
		if err != nil {
			return err
		}
		if device.RevokedAt.Valid {
			return httpapi.NewError(http.StatusForbidden, httpapi.CodeDeviceRevoked, "Device has been revoked.")
		}

		operations := sortedOperations(req.Operations)
		for start := 0; start < len(operations); {
			noteID := operations[start].NoteID
			end := start + 1
			for end < len(operations) && operations[end].NoteID == noteID {
				end++
			}

			// Each note batch is independently accepted or rejected. A rejected
			// operation rolls back the whole note batch without discarding other
			// note batches in the request.
			accepted, rejected, err := s.applyOperationBatch(ctx, tx, ownerID, req.DeviceID, operations[start:end])
			if err != nil {
				return err
			}
			if rejected != nil {
				resp.Rejected = append(resp.Rejected, *rejected)
			} else if accepted != nil {
				resp.Accepted = append(resp.Accepted, *accepted)
			}
			start = end
		}

		// Pull one extra row to discover whether another page exists.
		changes, err := tx.GetChangesAfterCursor(ctx, ownerID, req.Cursor, req.DeviceID, limit+1)
		if err != nil {
			return err
		}
		if int32(len(changes)) > limit {
			resp.HasMore = true
			changes = changes[:limit]
		}
		resp.Changes = make([]PulledChange, 0, len(changes))
		for _, change := range changes {
			var pulled PulledChange
			if err := pulled.FromEntity(change); err != nil {
				return err
			}
			resp.Changes = append(resp.Changes, pulled)
		}

		// Cursor update is committed with the same transaction as operation
		// application and pull calculation.
		resp.NextCursor = nextCursor(req.Cursor, resp.Changes, resp.HasMore)
		return tx.UpdateDeviceCursor(ctx, req.DeviceID, ownerID, resp.NextCursor)
	})
	if err != nil {
		return Response{}, err
	}

	s.logger.InfoContext(ctx, "sync completed",
		"user_id", ownerID.String(),
		"device_id", req.DeviceID.String(),
		"accepted", len(resp.Accepted),
		"rejected", len(resp.Rejected),
		"pulled", len(resp.Changes),
		"has_more", resp.HasMore,
	)
	return resp, nil
}

// sortedOperations returns a copy ordered by note id and then client sequence.
// Equal note/sequence pairs keep request order.
func sortedOperations(operations []ClientOperation) []ClientOperation {
	sorted := append([]ClientOperation(nil), operations...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if cmp := bytes.Compare(sorted[i].NoteID[:], sorted[j].NoteID[:]); cmp != 0 {
			return cmp < 0
		}
		return sorted[i].Sequence < sorted[j].Sequence
	})
	return sorted
}

// applyOperationBatch applies all operations for one note under a savepoint. A
// single rejected operation rolls back the whole note batch and returns one
// rejection entry with the current server note snapshot.
func (s *Service) applyOperationBatch(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, operations []ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	if len(operations) == 0 {
		return nil, nil, nil
	}
	operations = sortedOperations(operations)

	var document *store.NoteDocument
	if operations[0].NoteID != uuid.Nil {
		loaded, err := tx.GetNoteDocumentForOwnerForUpdate(ctx, operations[0].NoteID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			document = nil
		} else if err != nil {
			return nil, nil, err
		} else {
			document = &loaded
		}
	}
	originalDocument := cloneNoteDocument(document)

	var failed RejectedDTO
	changed := false

	err := tx.WithSavepoint(ctx, func(batchTx *store.Store) error {
		for _, op := range operations {
			var rejected *RejectedDTO
			var operationChanged bool
			var err error
			document, _, rejected, operationChanged, err = s.applyOperation(ctx, batchTx, ownerID, deviceID, document, op)
			if err != nil {
				return err
			}
			changed = changed || operationChanged

			if rejected != nil {
				failed = *rejected
				return errOperationBatchRejected
			}
		}
		return nil
	})
	if errors.Is(err, errOperationBatchRejected) {
		rejected := rejectedBatchDTO(operations, failed, originalDocument)
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if changed && document != nil {
		document.Note.CurrentVersion++
		if err := tx.SaveNoteDocument(ctx, *document); err != nil {
			return nil, nil, err
		}
		if err := s.enqueueSnapshotIfNeeded(ctx, tx, document.Note.ID, ownerID, document.Note.CurrentVersion); err != nil {
			return nil, nil, err
		}
	}

	noteVersion := int64(0)
	if document != nil {
		noteVersion = document.Note.CurrentVersion
	}
	accepted := AcceptedDTO{
		NoteID:            operations[0].NoteID,
		ServerNoteVersion: noteVersion,
	}
	return &accepted, nil, nil
}
func rejectedBatchDTO(operations []ClientOperation, failed RejectedDTO, document *store.NoteDocument) RejectedDTO {
	rejected := failed
	rejected.NoteID = operations[0].NoteID
	if operations[0].NoteID == uuid.Nil {
		return rejected
	}
	if document == nil {
		return rejected
	}
	var snapshot NoteSnapshot
	if err := snapshot.FromEntity(*document); err != nil {
		return rejected
	}
	rejected.NoteSnapshot = &snapshot
	if rejected.ServerNoteVersion == 0 {
		rejected.ServerNoteVersion = document.Note.CurrentVersion
	}
	return rejected
}

func cloneNoteDocument(document *store.NoteDocument) *store.NoteDocument {
	if document == nil {
		return nil
	}
	clone := *document
	clone.Note.NoteProperties = append(json.RawMessage(nil), document.Note.NoteProperties...)
	clone.Blocks = make([]store.NoteBlock, len(document.Blocks))
	for i, block := range document.Blocks {
		clone.Blocks[i] = block
		clone.Blocks[i].BlockProperties = append(json.RawMessage(nil), block.BlockProperties...)
	}
	return &clone
}

// applyOperation handles one operation from a sync batch. It first checks the
// idempotency table, then validates shape and dispatches to the concrete
// operation implementation.
func (s *Service) applyOperation(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation) (*store.NoteDocument, *AcceptedDTO, *RejectedDTO, bool, error) {
	if processed, err := tx.FindProcessedOperation(ctx, deviceID, op.OperationID); err == nil {
		// A retry returns the original accepted result and does not mutate state
		// again.
		accepted := acceptedFromChange(processed)
		return document, &accepted, nil, false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return document, nil, nil, false, err
	}

	var operation Operation
	if err := operation.FromRequest(op); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, false, nil
	}
	switch op.OperationType {
	case OperationCreateNote:
		updated, accepted, rejected, err := s.createNote(ctx, tx, ownerID, deviceID, document, op)
		return updated, accepted, rejected, rejected == nil, err
	case OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		accepted, rejected, err := s.mutateNote(ctx, tx, ownerID, deviceID, document, op)
		return document, accepted, rejected, rejected == nil, err
	case OperationCreateBlock:
		accepted, rejected, err := s.createBlock(ctx, tx, ownerID, deviceID, document, op)
		return document, accepted, rejected, rejected == nil, err
	case OperationDeleteBlock, OperationModifyBlock:
		accepted, rejected, err := s.mutateBlock(ctx, tx, ownerID, deviceID, document, op)
		return document, accepted, rejected, rejected == nil, err
	case OperationCreateCategory:
		accepted, rejected, err := s.createCategory(ctx, tx, ownerID, deviceID, op)
		return document, accepted, rejected, rejected == nil, err
	case OperationDeleteCategory, OperationModifyCategory:
		accepted, rejected, err := s.mutateCategory(ctx, tx, ownerID, deviceID, op)
		return document, accepted, rejected, rejected == nil, err
	default:
		rejected := rejectedInvalid(op, "operationType is unsupported.")
		return document, nil, &rejected, false, nil
	}
}

func (s *Service) createCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var category Category
	if err := category.FromRequest(op.ChangeData, true); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := tx.CreateCategory(ctx, category.ID, ownerID, category.Name); err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) mutateCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var category Category
	if err := category.FromRequest(op.ChangeData, op.OperationType == OperationModifyCategory); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	err := tx.LockCategoryForOwner(ctx, category.ID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Category not found.")
		return nil, &rejected, nil
	} else if err != nil {
		return nil, nil, err
	}

	if op.OperationType == OperationModifyCategory {
		err = tx.UpdateCategory(ctx, category.ID, ownerID, category.Name)
	} else {
		err = tx.DeleteCategory(ctx, category.ID, ownerID)
	}
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createNote applies create_note. A new note must be based on version 0 and
// results in note version 1.
func (s *Service) createNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation) (*store.NoteDocument, *AcceptedDTO, *RejectedDTO, error) {
	if op.BaseNoteVersion != 0 {
		rejected := rejectedConflict(op, 0)
		return document, nil, &rejected, nil
	}
	if document != nil {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return document, nil, &rejected, nil
	}

	var model NoteModel
	if err := model.FromRequest(op, ownerID); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, nil
	}
	updatedValue, err := model.Entity()
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, nil
	}
	updated := &updatedValue
	accepted, _, err := s.acceptOperationChange(ctx, tx, ownerID, deviceID, updated, op, nil)
	if err != nil {
		return updated, nil, nil, err
	}
	return updated, accepted, nil, nil
}

// mutateNote applies update/delete operations for existing notes.
func (s *Service) mutateNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	if document == nil {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if document.Note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return nil, &rejected, nil
	}

	var mutation NoteMutation
	if err := mutation.FromRequest(op.ChangeData, op.OperationType); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}

	switch op.OperationType {
	case OperationModifyNoteProperty:
		noteProperties, err := mergeJSONProperties(document.Note.NoteProperties, mutation.NoteProperties, "noteProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		document.Note.NoteProperties = noteProperties
		return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, nil)

	case OperationModifyNoteTitle:
		title, err := applyTextDelta(document.Note.Title, mutation.TextChange.Text, mutation.TextChange.TextOperation, mutation.TextChange.Index)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		document.Note.Title = title
		return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, nil)

	case OperationDeleteNote:
		document.Note.DeletedAt.Valid = true
		document.Note.DeletedAt.Time = time.Now().UTC()
		return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, nil)
	}
	return nil, nil, errors.New("unsupported note operation")
}

func (s *Service) acceptOperationChange(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation, blockID *uuid.UUID) (*AcceptedDTO, *RejectedDTO, error) {
	baseVersion := document.Note.CurrentVersion
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, blockID, baseVersion, baseVersion+1)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createBlock applies create_block to the in-memory note document.
func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var blockModel BlockModel
	if err := blockModel.FromRequest(op.ChangeData); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	block, err := blockModel.Entity(op.NoteID)
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if document == nil {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if document.Note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return nil, &rejected, nil
	}
	for _, existing := range document.Blocks {
		if existing.ID == block.ID {
			rejected := rejectedInvalid(op, "block already exists.")
			return nil, &rejected, nil
		}
	}
	document.Blocks = append(document.Blocks, block)
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, &block.ID)
}

// mutateBlock applies update/delete operations to the in-memory note document.
func (s *Service) mutateBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var mutation BlockMutation
	if err := mutation.FromRequest(op.ChangeData, op.OperationType); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	blockID := mutation.ID

	if document == nil {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if document.Note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return nil, &rejected, nil
	}

	var block *store.NoteBlock
	for i := range document.Blocks {
		if document.Blocks[i].ID == blockID {
			block = &document.Blocks[i]
			break
		}
	}
	if block == nil {
		rejected := rejectedNotFound(op, "Block not found.")
		return nil, &rejected, nil
	}
	if mutation.HasProperties && len(mutation.ChangedProperties) > 0 {
		blockProperties, err := mergeJSONProperties(block.BlockProperties, mutation.ChangedProperties, "blockProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		block.BlockProperties = blockProperties
	}
	if mutation.HasTextChange {
		text, err := applyTextDelta(block.TextContent, mutation.TextChange.Text, mutation.TextChange.TextOperation, mutation.TextChange.Index)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		block.TextContent = text
	}
	if op.OperationType == OperationDeleteBlock {
		block.DeletedAt.Valid = true
		block.DeletedAt.Time = time.Now().UTC()
	}
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, &blockID)
}

// applyTextDelta applies an insert/delete to text using rune indexes. The
// delete length is the length of text.
func applyTextDelta(current, delta, operationType string, index int) (string, error) {
	if index < 0 {
		return "", errors.New("index must be greater than or equal to zero")
	}
	textRunes := []rune(current)
	deltaRunes := []rune(delta)
	switch operationType {
	case TextOperationInsert:
		if index > len(textRunes) {
			return "", errors.New("index is outside the current text")
		}
		next := make([]rune, 0, len(textRunes)+len(deltaRunes))
		next = append(next, textRunes[:index]...)
		next = append(next, deltaRunes...)
		next = append(next, textRunes[index:]...)
		return string(next), nil
	case TextOperationDelete:
		if index > len(textRunes) || index+len(deltaRunes) > len(textRunes) {
			return "", errors.New("delete range is outside the current text")
		}
		next := make([]rune, 0, len(textRunes)-len(deltaRunes))
		next = append(next, textRunes[:index]...)
		next = append(next, textRunes[index+len(deltaRunes):]...)
		return string(next), nil
	default:
		return "", errors.New("textOperation must be insert or delete")
	}
}

func mergeJSONProperties(current json.RawMessage, changes map[string]json.RawMessage, name string) (json.RawMessage, error) {
	merged, err := decodeObjectFields(store.NormalizeJSON(current), name)
	if err != nil {
		return nil, err
	}
	for key, value := range changes {
		merged[key] = value
	}
	return json.Marshal(merged)
}

// insertChange writes the append-only history row for an accepted operation.
func (s *Service) insertChange(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, blockID *uuid.UUID, baseNoteVersion, resultingNoteVersion int64) (store.NoteChange, error) {
	params, err := op.Entity(ownerID, deviceID, blockID, baseNoteVersion, resultingNoteVersion)
	if err != nil {
		return store.NoteChange{}, err
	}
	return tx.InsertNoteChange(ctx, params)
}

// enqueueSnapshotIfNeeded evaluates configured thresholds and inserts a snapshot
// outbox job when the note has enough unsnapshotted history.
func (s *Service) enqueueSnapshotIfNeeded(ctx context.Context, tx *store.Store, noteID, ownerID uuid.UUID, resultingVersion int64) error {
	if s.snapshotChangeThreshold <= 0 && s.snapshotByteThreshold <= 0 {
		return nil
	}
	changeCount, err := tx.CountChangesSinceLastSnapshot(ctx, noteID)
	if err != nil {
		return err
	}
	changeBytes, err := tx.SumChangeBytesSinceLastSnapshot(ctx, noteID)
	if err != nil {
		return err
	}
	if (s.snapshotChangeThreshold > 0 && changeCount >= s.snapshotChangeThreshold) ||
		(s.snapshotByteThreshold > 0 && changeBytes >= s.snapshotByteThreshold) {
		_, err := tx.EnqueueSnapshotJob(ctx, noteID, ownerID, resultingVersion)
		return err
	}
	return nil
}

// acceptedFromChange maps a stored change to the accepted response shape. It is
// used for both first-time accepts and idempotent retries.
func acceptedFromChange(change store.NoteChange) AcceptedDTO {
	return AcceptedDTO{
		NoteID:            change.NoteID,
		ServerNoteVersion: change.ResultingNoteVersion,
	}
}

// nextCursor calculates the cursor the client should persist after this
// response. When a pull page has more rows, the cursor must stop at the last
// returned pulled change so the next page is not skipped.
func nextCursor(start int64, changes []PulledChange, hasMore bool) int64 {
	if hasMore && len(changes) > 0 {
		return changes[len(changes)-1].Sequence
	}
	next := start
	for _, change := range changes {
		if change.Sequence > next {
			next = change.Sequence
		}
	}
	return next
}

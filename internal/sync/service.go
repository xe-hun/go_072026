package syncapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

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
	if req.ProtocolVersion != ProtocolVersion {
		return Response{}, httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if req.DeviceID == uuid.Nil {
		return Response{}, httpapi.InvalidRequest("deviceId is required.")
	}
	if len(req.Operations) > s.maxOperations {
		return Response{}, httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The sync request contains too many operations.")
	}

	limit := req.Limit
	if limit == 0 {
		// Defaulting keeps clients simple while still bounding pull response size.
		limit = s.defaultPullLimit
	}
	if limit < 1 {
		return Response{}, httpapi.InvalidRequest("limit must be greater than zero.")
	}
	if limit > s.maxPullLimit {
		limit = s.maxPullLimit
	}

	resp := Response{
		Accepted:   []AcceptedOperation{},
		Rejected:   []RejectedOperation{},
		Changes:    []PulledChange{},
		NextCursor: req.Cursor,
	}

	err := s.store.WithTx(ctx, func(tx *store.Store) error {
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
			resp.Changes = append(resp.Changes, mapPulledChange(change))
		}

		// Cursor update is committed with the same transaction as operation
		// application and pull calculation.
		resp.NextCursor = nextCursor(req.Cursor, resp.Accepted, resp.Changes, resp.HasMore)
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
func (s *Service) applyOperationBatch(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, operations []ClientOperation) (*AcceptedOperation, *RejectedOperation, error) {
	if len(operations) == 0 {
		return nil, nil, nil
	}

	acceptedItems := make([]AcceptedOperation, 0, len(operations))
	var failed RejectedOperation
	err := tx.WithSavepoint(ctx, func(batchTx *store.Store) error {
		for _, op := range operations {
			accepted, rejected, err := s.applyOperation(ctx, batchTx, ownerID, deviceID, op)
			if err != nil {
				return err
			}
			if rejected != nil {
				failed = *rejected
				return errOperationBatchRejected
			}
			if accepted != nil {
				acceptedItems = append(acceptedItems, *accepted)
			}
		}
		return nil
	})
	if errors.Is(err, errOperationBatchRejected) {
		rejected, err := s.rejectedOperationBatch(ctx, tx, ownerID, operations, failed)
		if err != nil {
			return nil, nil, err
		}
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}

	accepted, err := acceptedOperationBatch(operations, acceptedItems)
	if err != nil {
		return nil, nil, err
	}
	return &accepted, nil, nil
}

func acceptedOperationBatch(operations []ClientOperation, acceptedItems []AcceptedOperation) (AcceptedOperation, error) {
	if len(acceptedItems) == 0 {
		return AcceptedOperation{
			OperationIDs: operationIDs(operations),
			NoteID:       operations[0].NoteID,
		}, nil
	}
	if operations[0].NoteID == uuid.Nil {
		return AcceptedOperation{
			OperationID:  acceptedItems[len(acceptedItems)-1].OperationID,
			OperationIDs: operationIDs(operations),
			Sequence:     maxAcceptedSequence(acceptedItems),
		}, nil
	}

	last := acceptedItems[len(acceptedItems)-1]
	accepted := AcceptedOperation{
		OperationID:  last.OperationID,
		OperationIDs: operationIDs(operations),
		NoteID:       operations[0].NoteID,
		NoteVersion:  last.NoteVersion,
		Sequence:     maxAcceptedSequence(acceptedItems),
	}
	if len(acceptedItems) == 1 {
		accepted.BlockID = last.BlockID
	}
	return accepted, nil
}

func (s *Service) rejectedOperationBatch(ctx context.Context, tx *store.Store, ownerID uuid.UUID, operations []ClientOperation, failed RejectedOperation) (RejectedOperation, error) {
	rejected := failed
	rejected.OperationIDs = operationIDs(operations)
	rejected.NoteID = operations[0].NoteID
	if operations[0].NoteID == uuid.Nil {
		return rejected, nil
	}

	doc, err := tx.GetNoteDocument(ctx, operations[0].NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		return rejected, nil
	}
	if err != nil {
		return RejectedOperation{}, err
	}
	snapshot := mapNoteSnapshot(doc)
	rejected.NoteSnapshot = &snapshot
	if rejected.ServerNoteVersion == 0 {
		rejected.ServerNoteVersion = doc.Note.CurrentVersion
	}
	return rejected, nil
}

func operationIDs(operations []ClientOperation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(operations))
	for _, op := range operations {
		ids = append(ids, op.OperationID)
	}
	return ids
}

func maxAcceptedSequence(items []AcceptedOperation) int64 {
	var sequence int64
	for _, item := range items {
		if item.Sequence > sequence {
			sequence = item.Sequence
		}
	}
	return sequence
}

// applyOperation handles one operation from a sync batch. It first checks the
// idempotency table, then validates shape and dispatches to the concrete
// operation implementation.
func (s *Service) applyOperation(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation) (*AcceptedOperation, *RejectedOperation, error) {
	if processed, err := tx.FindProcessedOperation(ctx, deviceID, op.OperationID); err == nil {
		// A retry returns the original accepted result and does not mutate state
		// again.
		accepted := acceptedFromChange(processed)
		return &accepted, nil, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}

	if err := validateOperationShape(op); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	fields, err := decodeChangeObject(op.ChangeData)
	if err != nil {
		rejected := rejectedInvalid(op, "changeData is invalid.")
		return nil, &rejected, nil
	}

	switch op.OperationType {
	case OperationCreateNote:
		return s.createNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		return s.mutateNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationCreateBlock:
		return s.createBlock(ctx, tx, ownerID, deviceID, op, fields)
	case OperationDeleteBlock, OperationModifyBlockProperty:
		return s.mutateBlock(ctx, tx, ownerID, deviceID, op, fields)
	case OperationCreateCategory:
		return s.createCategory(ctx, tx, ownerID, deviceID, op, fields)
	case OperationDeleteCategory, OperationModifyCategory:
		return s.mutateCategory(ctx, tx, ownerID, deviceID, op, fields)
	default:
		rejected := rejectedInvalid(op, "operationType is unsupported.")
		return nil, &rejected, nil
	}
}

func categoryIDAndName(fields map[string]json.RawMessage, requireName bool) (uuid.UUID, string, error) {
	id, ok, err := getUUIDField(fields, "id")
	if err != nil {
		return uuid.Nil, "", err
	}
	if !ok || id == uuid.Nil {
		return uuid.Nil, "", errors.New("id is required")
	}
	if !requireName {
		return id, "", nil
	}
	name, ok, err := getStringField(fields, "name")
	if err != nil {
		return uuid.Nil, "", err
	}
	if !ok || strings.TrimSpace(name) == "" {
		return uuid.Nil, "", errors.New("name is required")
	}
	return id, name, nil
}

func (s *Service) createCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	id, name, err := categoryIDAndName(fields, true)
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := tx.CreateCategory(ctx, id, ownerID, name); err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, nil, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) mutateCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	id, name, err := categoryIDAndName(fields, op.OperationType == OperationModifyCategory)
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := tx.LockCategoryForOwner(ctx, id, ownerID); errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Category not found.")
		return nil, &rejected, nil
	} else if err != nil {
		return nil, nil, err
	}

	if op.OperationType == OperationModifyCategory {
		err = tx.UpdateCategory(ctx, id, ownerID, name)
	} else {
		err = tx.DeleteCategory(ctx, id, ownerID)
	}
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, nil, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createNote applies create_note. A new note must be based on version 0 and
// results in note version 1.
func (s *Service) createNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	if op.BaseNoteVersion != 0 {
		rejected := rejectedConflict(op, 0)
		return nil, &rejected, nil
	}

	title, ok, err := getStringField(fields, "title")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		title = ""
	}
	metadata, ok, err := getObjectField(fields, "metadata")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		metadata = json.RawMessage(`{}`)
	}
	err = tx.CreateNote(ctx, store.Note{
		ID:             op.NoteID,
		OwnerID:        ownerID,
		Title:          title,
		Metadata:       metadata,
		CurrentVersion: 1,
	})
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, &op.NoteID, nil, 0, 1)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, op.NoteID, ownerID, 1); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// mutateNote applies update/delete operations for existing notes.
func (s *Service) mutateNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	switch op.OperationType {
	case OperationModifyNoteProperty:
		metadataRaw, ok, err := getObjectField(fields, "metaData")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if !ok {
			rejected := rejectedInvalid(op, "modify_note_property must include at least one property.")
			return nil, &rejected, nil
		}
		changes, err := decodeObjectFields(metadataRaw, "metaData")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if len(changes) == 0 {
			rejected := rejectedInvalid(op, "modify_note_property must include at least one property.")
			return nil, &rejected, nil
		}
		metadata, baseVersion, err := tx.GetNoteMetadataForOwnerForUpdate(ctx, op.NoteID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Note not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if baseVersion != op.BaseNoteVersion {
			rejected := rejectedConflict(op, baseVersion)
			return nil, &rejected, nil
		}
		metadata, err = mergeJSONProperties(metadata, changes, "metadata")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		resultingVersion := baseVersion + 1
		if err := tx.UpdateNoteMetadata(ctx, op.NoteID, ownerID, metadata, resultingVersion); err != nil {
			return nil, nil, err
		}
		return s.acceptNoteMutation(ctx, tx, ownerID, deviceID, op, &op.NoteID, baseVersion, resultingVersion)

	case OperationModifyNoteTitle:
		textDelta, ok, err := getObjectField(fields, "textDelta")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if !ok {
			rejected := rejectedInvalid(op, "modify_note_title requires textDelta.")
			return nil, &rejected, nil
		}
		textChange, err := decodeTextChange(textDelta, "textDelta")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		title, baseVersion, err := tx.GetNoteTitleForOwnerForUpdate(ctx, op.NoteID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Note not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if baseVersion != op.BaseNoteVersion {
			rejected := rejectedConflict(op, baseVersion)
			return nil, &rejected, nil
		}
		title, err = applyTextDelta(title, textChange.Text, textChange.TextOperation, textChange.Index)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		resultingVersion := baseVersion + 1
		if err := tx.UpdateNoteTitle(ctx, op.NoteID, ownerID, title, resultingVersion); err != nil {
			return nil, nil, err
		}
		return s.acceptNoteMutation(ctx, tx, ownerID, deviceID, op, &op.NoteID, baseVersion, resultingVersion)

	case OperationDeleteNote:
		baseVersion, err := tx.GetNoteVersionForOwnerForUpdate(ctx, op.NoteID, ownerID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Note not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if baseVersion != op.BaseNoteVersion {
			rejected := rejectedConflict(op, baseVersion)
			return nil, &rejected, nil
		}
		resultingVersion := baseVersion + 1
		if err := tx.DeleteNote(ctx, op.NoteID, ownerID, resultingVersion); err != nil {
			return nil, nil, err
		}
		return s.acceptNoteMutation(ctx, tx, ownerID, deviceID, op, &op.NoteID, baseVersion, resultingVersion)
	}
	return nil, nil, errors.New("unsupported note operation")
}

func (s *Service) acceptNoteMutation(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, noteID *uuid.UUID, baseVersion, resultingVersion int64) (*AcceptedOperation, *RejectedOperation, error) {
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, noteID, nil, baseVersion, resultingVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, *noteID, ownerID, resultingVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createBlock applies create_block and increments the parent note version.
func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	position, ok, err := getIntField(fields, "position")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "position is required.")
		return nil, &rejected, nil
	}
	if position < 0 {
		rejected := rejectedInvalid(op, "position must be greater than or equal to zero.")
		return nil, &rejected, nil
	}
	blockRaw, ok, err := getObjectField(fields, "block")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "block is required.")
		return nil, &rejected, nil
	}
	blockFields, err := decodeObjectFields(blockRaw, "block")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}

	blockType, ok, err := getStringField(blockFields, "blockType")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "block.blockType is required.")
		return nil, &rejected, nil
	}
	if err := validateBlockType(blockType); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	text, ok, err := getStringField(blockFields, "textContent")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}

	properties, ok, err := getObjectField(blockFields, "properties")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		properties = json.RawMessage(`{}`)
	}

	baseNoteVersion, err := tx.GetNoteVersionForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if baseNoteVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, baseNoteVersion)
		return nil, &rejected, nil
	}
	resultingVersion := baseNoteVersion + 1
	if err := tx.IncrementNoteVersion(ctx, op.NoteID, ownerID, resultingVersion); err != nil {
		return nil, nil, err
	}

	if err := tx.CreateBlock(ctx, store.NoteBlock{
		ID:          *op.BlockID,
		NoteID:      op.NoteID,
		BlockType:   blockType,
		TextContent: text,
		Position:    strconv.Itoa(position),
		Properties:  properties,
	}); err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, &op.NoteID, op.BlockID, baseNoteVersion, resultingVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, op.NoteID, ownerID, resultingVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// mutateBlock applies update/delete operations for existing blocks.
func (s *Service) mutateBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	var (
		changedProperties map[string]json.RawMessage
		textChange        TextChange
		hasProperties     bool
		hasText           bool
		position          int
	)

	if op.OperationType == OperationModifyBlockProperty {
		changedPropertiesRaw, hasChangedProperties, err := getObjectField(fields, "changedProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if hasChangedProperties {
			changedProperties, err = decodeObjectFields(changedPropertiesRaw, "changedProperties")
			hasProperties = true
		}
		textDeltaRaw, hasTextDelta, err := getObjectField(fields, "textDelta")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if hasTextDelta {
			textChange, err = decodeTextChange(textDeltaRaw, "textDelta")
			hasText = true
		}
		if (!hasProperties || len(changedProperties) == 0) && !hasText {
			rejected := rejectedInvalid(op, "modify_block_property must include changedProperties or textDelta.")
			return nil, &rejected, nil
		}
	}
	if op.OperationType == OperationDeleteBlock {
		var ok bool
		var err error
		position, ok, err = getIntField(fields, "position")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if !ok || position < 0 {
			rejected := rejectedInvalid(op, "position must be a non-negative integer.")
			return nil, &rejected, nil
		}
	}

	baseNoteVersion, err := tx.GetNoteVersionForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if baseNoteVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, baseNoteVersion)
		return nil, &rejected, nil
	}

	var properties json.RawMessage
	var text string
	if op.OperationType == OperationDeleteBlock {
		if err := tx.LockBlockForUpdate(ctx, op.NoteID, *op.BlockID, strconv.Itoa(position)); errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Block not found.")
			return nil, &rejected, nil
		} else if err != nil {
			return nil, nil, err
		}
	}
	if hasProperties {
		properties, err = tx.GetBlockPropertiesForUpdate(ctx, op.NoteID, *op.BlockID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Block not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		properties, err = mergeJSONProperties(properties, changedProperties, "properties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
	}
	if hasText {
		text, err = tx.GetBlockTextForUpdate(ctx, op.NoteID, *op.BlockID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := rejectedNotFound(op, "Block not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		text, err = applyTextDelta(text, textChange.Text, textChange.TextOperation, textChange.Index)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
	}

	resultingVersion := baseNoteVersion + 1
	if err := tx.IncrementNoteVersion(ctx, op.NoteID, ownerID, resultingVersion); err != nil {
		return nil, nil, err
	}
	if hasProperties {
		if err := tx.UpdateBlockProperties(ctx, op.NoteID, *op.BlockID, properties); err != nil {
			return nil, nil, err
		}
	}
	if hasText {
		if err := tx.UpdateBlockText(ctx, op.NoteID, *op.BlockID, text); err != nil {
			return nil, nil, err
		}
	}
	if op.OperationType == OperationDeleteBlock {
		if err := tx.DeleteBlock(ctx, op.NoteID, *op.BlockID); err != nil {
			return nil, nil, err
		}
	}

	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, &op.NoteID, op.BlockID, baseNoteVersion, resultingVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, op.NoteID, ownerID, resultingVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
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
func (s *Service) insertChange(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, noteID, blockID *uuid.UUID, baseNoteVersion, resultingNoteVersion int64) (store.NoteChange, error) {
	return tx.InsertNoteChange(ctx, store.InsertNoteChangeParams{
		ID:                   uuid.New(),
		OwnerID:              ownerID,
		NoteID:               noteID,
		BlockID:              blockID,
		DeviceID:             deviceID,
		ClientOperationID:    op.OperationID,
		OperationType:        op.OperationType,
		BaseNoteVersion:      baseNoteVersion,
		ResultingNoteVersion: resultingNoteVersion,
		ChangeFormat:         normalizeChangeFormat(op.ChangeFormat),
		SchemaVersion:        1,
		ChangeData:           op.ChangeData,
	})
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
func acceptedFromChange(change store.NoteChange) AcceptedOperation {
	return AcceptedOperation{
		OperationID: change.ClientOperationID,
		NoteID:      change.NoteID,
		BlockID:     store.UUIDPtr(change.BlockID),
		NoteVersion: change.ResultingNoteVersion,
		Sequence:    change.GlobalSequence,
	}
}

// mapPulledChange maps a change-log row into the pull response shape.
func mapPulledChange(change store.NoteChange) PulledChange {
	return PulledChange{
		ID:                   change.ID,
		OperationID:          change.ClientOperationID,
		NoteID:               change.NoteID,
		BlockID:              store.UUIDPtr(change.BlockID),
		DeviceID:             change.DeviceID,
		OperationType:        change.OperationType,
		BaseNoteVersion:      change.BaseNoteVersion,
		ResultingNoteVersion: change.ResultingNoteVersion,
		ChangeFormat:         change.ChangeFormat,
		SchemaVersion:        change.SchemaVersion,
		ChangeData:           store.NormalizeJSON(change.ChangeData),
		Sequence:             change.GlobalSequence,
		CreatedAt:            change.CreatedAt,
	}
}

// mapNoteSnapshot maps current note state into the sync rejection payload.
func mapNoteSnapshot(doc store.NoteDocument) NoteSnapshot {
	blocks := make([]BlockSnapshot, 0, len(doc.Blocks))
	for _, block := range doc.Blocks {
		blocks = append(blocks, BlockSnapshot{
			ID:          block.ID,
			NoteID:      block.NoteID,
			BlockType:   block.BlockType,
			TextContent: block.TextContent,
			Position:    block.Position,
			Properties:  store.NormalizeJSON(block.Properties),
			CreatedAt:   block.CreatedAt,
			UpdatedAt:   block.UpdatedAt,
			DeletedAt:   store.TimePtr(block.DeletedAt),
		})
	}
	return NoteSnapshot{
		ID:             doc.Note.ID,
		OwnerID:        doc.Note.OwnerID,
		Title:          doc.Note.Title,
		Metadata:       store.NormalizeJSON(doc.Note.Metadata),
		CurrentVersion: doc.Note.CurrentVersion,
		CreatedAt:      doc.Note.CreatedAt,
		UpdatedAt:      doc.Note.UpdatedAt,
		DeletedAt:      store.TimePtr(doc.Note.DeletedAt),
		Blocks:         blocks,
	}
}

// nextCursor calculates the cursor the client should persist after this
// response. When a pull page has more rows, the cursor must stop at the last
// returned pulled change so the next page is not skipped.
func nextCursor(start int64, accepted []AcceptedOperation, changes []PulledChange, hasMore bool) int64 {
	if hasMore && len(changes) > 0 {
		return changes[len(changes)-1].Sequence
	}
	next := start
	for _, item := range accepted {
		if item.Sequence > next {
			next = item.Sequence
		}
	}
	for _, change := range changes {
		if change.Sequence > next {
			next = change.Sequence
		}
	}
	return next
}

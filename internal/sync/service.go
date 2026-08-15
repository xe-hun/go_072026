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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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

	accepted, err := s.acceptedOperationBatch(ctx, tx, ownerID, operations, acceptedItems)
	if err != nil {
		return nil, nil, err
	}
	return &accepted, nil, nil
}

func (s *Service) acceptedOperationBatch(ctx context.Context, tx *store.Store, ownerID uuid.UUID, operations []ClientOperation, acceptedItems []AcceptedOperation) (AcceptedOperation, error) {
	if len(acceptedItems) == 0 {
		return AcceptedOperation{
			OperationIDs: operationIDs(operations),
			NoteID:       operations[0].NoteID,
		}, nil
	}
	note, err := tx.GetNoteForOwner(ctx, operations[0].NoteID, ownerID)
	if err != nil {
		return AcceptedOperation{}, err
	}

	last := acceptedItems[len(acceptedItems)-1]
	accepted := AcceptedOperation{
		OperationID:  last.OperationID,
		OperationIDs: operationIDs(operations),
		NoteID:       operations[0].NoteID,
		NoteVersion:  note.CurrentVersion,
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
	op.OperationType = normalizeOperationType(op.OperationType)

	fields, err := decodeChangeObject(op.ChangeData)
	if err != nil {
		rejected := rejectedInvalid(op, "changeData is invalid.")
		return nil, &rejected, nil
	}

	switch op.OperationType {
	case OperationCreateNote:
		return s.createNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationUpdateNote, OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		return s.mutateNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationCreateBlock:
		return s.createBlock(ctx, tx, ownerID, deviceID, op, fields)
	case OperationUpdateBlock, OperationDeleteBlock, OperationModifyBlockProperty:
		return s.mutateBlock(ctx, tx, ownerID, deviceID, op, fields)
	default:
		rejected := rejectedInvalid(op, "operationType is unsupported.")
		return nil, &rejected, nil
	}
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
	categoryID, ok, err := getNullableUUIDField(fields, "categoryId")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		categoryID = pgtype.UUID{}
	}

	created, err := tx.CreateNote(ctx, store.Note{
		ID:             op.NoteID,
		OwnerID:        ownerID,
		CategoryID:     categoryID,
		Title:          title,
		Metadata:       metadata,
		CurrentVersion: 1,
	})
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, 0, created.CurrentVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, created.ID, ownerID, created.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// mutateNote applies update/delete operations for existing notes.
func (s *Service) mutateNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	// The note row is locked before version checks so concurrent edits to the
	// same note serialize.
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		// The client edited stale state. Return an explicit conflict rather than
		// overwriting.
		rejected := rejectedConflict(op, note.CurrentVersion)
		return nil, &rejected, nil
	}

	baseVersion := note.CurrentVersion
	// Every note-level mutation increments the note version.
	note.CurrentVersion++

	switch op.OperationType {
	case OperationUpdateNote:
		if title, ok, err := getStringField(fields, "title"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			note.Title = title
		}
		if metadata, ok, err := getObjectField(fields, "metadata"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			note.Metadata = metadata
		}
		if categoryID, ok, err := getNullableUUIDField(fields, "categoryId"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			note.CategoryID = categoryID
		}
	case OperationModifyNoteProperty:
		if len(fields) == 0 {
			rejected := rejectedInvalid(op, "modify_note_property must include at least one property.")
			return nil, &rejected, nil
		}
		metadata, err := mergeJSONProperties(note.Metadata, fields, "metadata")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		note.Metadata = metadata
	case OperationModifyNoteTitle:
		textChange, err := decodeTextChange(op.ChangeData, "changeData")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		title, err := applyTextDelta(note.Title, textChange.Text, textChange.TextOperation, textChange.Index)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		note.Title = title
	case OperationDeleteNote:
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
		// Deletion is soft: the row remains so tombstones can sync to offline
		// devices.
		note.DeletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}

	updated, err := tx.UpdateNoteState(ctx, note)
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, baseVersion, updated.CurrentVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, updated.ID, ownerID, updated.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createBlock applies create_block and increments the parent note version.
func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	// Lock the parent note first because the note version is the global version
	// for all block mutations within the note.
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, note.CurrentVersion)
		return nil, &rejected, nil
	}

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
	blockSnapshot, blockFields, err := decodeBlockSnapshot(blockRaw)
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if blockSnapshot.ID != uuid.Nil && blockSnapshot.ID != *op.BlockID {
		rejected := rejectedInvalid(op, "block.id must match blockId.")
		return nil, &rejected, nil
	}
	if blockSnapshot.NoteID != uuid.Nil && blockSnapshot.NoteID != op.NoteID {
		rejected := rejectedInvalid(op, "block.noteId must match noteId.")
		return nil, &rejected, nil
	}
	blockType := blockSnapshot.BlockType
	if blockType == "" {
		blockType, _, err = getStringField(blockFields, "type")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
	}
	if blockType == "" {
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
	if !ok {
		text, _, err = getStringField(blockFields, "text")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
	}
	properties, ok, err := getObjectField(blockFields, "properties")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		properties = json.RawMessage(`{}`)
	}

	baseNoteVersion := note.CurrentVersion
	// A block create is also a note mutation.
	note.CurrentVersion++
	if _, err := tx.UpdateNoteState(ctx, note); err != nil {
		return nil, nil, err
	}

	block, err := tx.CreateBlock(ctx, store.NoteBlock{
		ID:          *op.BlockID,
		NoteID:      note.ID,
		BlockType:   blockType,
		TextContent: text,
		Position:    strconv.Itoa(position),
		Properties:  properties,
	})
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, &block.ID, baseNoteVersion, note.CurrentVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, note.ID, ownerID, note.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// mutateBlock applies update/delete operations for existing blocks.
func (s *Service) mutateBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	// Lock order is note first, block second. Keeping this order everywhere avoids
	// avoidable deadlocks when multiple API instances edit the same note.
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, note.CurrentVersion)
		return nil, &rejected, nil
	}

	block, err := tx.GetBlockForNoteForUpdate(ctx, note.ID, *op.BlockID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Block not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}

	baseNoteVersion := note.CurrentVersion
	// A block mutation increments the parent note version.
	note.CurrentVersion++

	switch op.OperationType {
	case OperationUpdateBlock:
		changed := false
		if changedProperties, ok, err := getNullableObjectFields(fields, "changedProperties"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			properties, err := mergeJSONProperties(block.Properties, changedProperties, "properties")
			if err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
			block.Properties = properties
			changed = len(changedProperties) > 0
		}

		textChange, hasTextDelta, err := getNullableTextChangeField(fields, "textDelta")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if hasTextDelta {
			text, err := applyTextDelta(block.TextContent, textChange.Text, textChange.TextOperation, textChange.Index)
			if err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
			block.TextContent = text
			changed = true
		}
		if !changed {
			rejected := rejectedInvalid(op, "update_block must include at least one supported field.")
			return nil, &rejected, nil
		}
	case OperationModifyBlockProperty:
		if len(fields) == 0 {
			rejected := rejectedInvalid(op, "modify_block_property must include at least one property.")
			return nil, &rejected, nil
		}
		properties, err := mergeJSONProperties(block.Properties, fields, "properties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		block.Properties = properties
	case OperationDeleteBlock:
		// Deletion is soft so offline devices can receive the tombstone later.
		block.DeletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}

	if _, err := tx.UpdateNoteState(ctx, note); err != nil {
		return nil, nil, err
	}
	if _, err := tx.UpdateBlockState(ctx, block); err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, op.BlockID, baseNoteVersion, note.CurrentVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, note.ID, ownerID, note.CurrentVersion); err != nil {
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

func decodeBlockSnapshot(raw json.RawMessage) (BlockSnapshot, map[string]json.RawMessage, error) {
	fields, err := decodeObjectFields(raw, "block")
	if err != nil {
		return BlockSnapshot{}, nil, err
	}
	block := BlockSnapshot{}
	if rawID, ok := fields["id"]; ok && !isJSONNull(rawID) {
		if err := json.Unmarshal(rawID, &block.ID); err != nil {
			return BlockSnapshot{}, nil, errors.New("block.id must be a UUID")
		}
	}
	if rawNoteID, ok := fields["noteId"]; ok && !isJSONNull(rawNoteID) {
		if err := json.Unmarshal(rawNoteID, &block.NoteID); err != nil {
			return BlockSnapshot{}, nil, errors.New("block.noteId must be a UUID")
		}
	}
	blockType, _, err := getStringField(fields, "blockType")
	if err != nil {
		return BlockSnapshot{}, nil, err
	}
	block.BlockType = blockType
	text, _, err := getStringField(fields, "textContent")
	if err != nil {
		return BlockSnapshot{}, nil, err
	}
	block.TextContent = text
	return block, fields, nil
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
	return tx.InsertNoteChange(ctx, store.InsertNoteChangeParams{
		ID:                   uuid.New(),
		OwnerID:              ownerID,
		NoteID:               op.NoteID,
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
		CategoryID:     store.UUIDPtr(doc.Note.CategoryID),
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

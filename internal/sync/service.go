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
		Accepted:   []AcceptedDTO{},
		Rejected:   []RejectedDTO{},
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
	snapshot := mapNoteSnapshot(*document)
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

	if err := validateOperationShape(op); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, false, nil
	}
	fields, err := decodeChangeObject(op.ChangeData)
	if err != nil {
		rejected := rejectedInvalid(op, "changeData is invalid.")
		return document, nil, &rejected, false, nil
	}

	switch op.OperationType {
	case OperationCreateNote:
		updated, accepted, rejected, err := s.createNote(ctx, tx, ownerID, deviceID, document, op, fields)
		return updated, accepted, rejected, rejected == nil, err
	case OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		accepted, rejected, err := s.mutateNote(ctx, tx, ownerID, deviceID, document, op, fields)
		return document, accepted, rejected, rejected == nil, err
	case OperationCreateBlock:
		accepted, rejected, err := s.createBlock(ctx, tx, ownerID, deviceID, document, op, fields)
		return document, accepted, rejected, rejected == nil, err
	case OperationDeleteBlock, OperationModifyBlock:
		accepted, rejected, err := s.mutateBlock(ctx, tx, ownerID, deviceID, document, op, fields)
		return document, accepted, rejected, rejected == nil, err
	case OperationCreateCategory:
		accepted, rejected, err := s.createCategory(ctx, tx, ownerID, deviceID, op, fields)
		return document, accepted, rejected, rejected == nil, err
	case OperationDeleteCategory, OperationModifyCategory:
		accepted, rejected, err := s.mutateCategory(ctx, tx, ownerID, deviceID, op, fields)
		return document, accepted, rejected, rejected == nil, err
	default:
		rejected := rejectedInvalid(op, "operationType is unsupported.")
		return document, nil, &rejected, false, nil
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

func (s *Service) createCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedDTO, *RejectedDTO, error) {
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

func (s *Service) mutateCategory(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedDTO, *RejectedDTO, error) {
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
func (s *Service) createNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation, fields map[string]json.RawMessage) (*store.NoteDocument, *AcceptedDTO, *RejectedDTO, error) {
	if op.BaseNoteVersion != 0 {
		rejected := rejectedConflict(op, 0)
		return document, nil, &rejected, nil
	}
	if document != nil {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return document, nil, &rejected, nil
	}

	title, ok, err := getStringField(fields, "title")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "title is required.")
		return document, nil, &rejected, nil
	}
	noteProperties, ok, err := getObjectField(fields, "noteProperties")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "noteProperties is required.")
		return document, nil, &rejected, nil
	}
	blockValues, ok, err := getArrayField(fields, "blocks")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return document, nil, &rejected, nil
	}
	if !ok {
		rejected := rejectedInvalid(op, "blocks is required.")
		return document, nil, &rejected, nil
	}
	blocks := make([]store.NoteBlock, 0, len(blockValues))
	blockIDs := make(map[uuid.UUID]struct{}, len(blockValues))
	positions := make(map[string]struct{}, len(blockValues))
	for i, rawBlock := range blockValues {
		blockFields, err := decodeObjectFields(rawBlock, "blocks["+strconv.Itoa(i)+"]")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return document, nil, &rejected, nil
		}
		block, err := decodeBlockPayload(blockFields, op.NoteID)
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return document, nil, &rejected, nil
		}
		if _, exists := blockIDs[block.ID]; exists {
			rejected := rejectedInvalid(op, "blocks contains duplicate ids.")
			return document, nil, &rejected, nil
		}
		if _, exists := positions[block.Position]; exists {
			rejected := rejectedInvalid(op, "blocks contains duplicate positions.")
			return document, nil, &rejected, nil
		}
		blockIDs[block.ID] = struct{}{}
		positions[block.Position] = struct{}{}
		blocks = append(blocks, block)
	}
	updated := &store.NoteDocument{Note: store.Note{
		ID:             op.NoteID,
		OwnerID:        ownerID,
		Title:          title,
		NoteProperties: noteProperties,
		CurrentVersion: 0,
	}, Blocks: blocks}
	accepted, _, err := s.acceptOperationChange(ctx, tx, ownerID, deviceID, updated, op, nil)
	if err != nil {
		return updated, nil, nil, err
	}
	return updated, accepted, nil, nil
}

func decodeBlockPayload(fields map[string]json.RawMessage, noteID uuid.UUID) (store.NoteBlock, error) {
	id, ok, err := getUUIDField(fields, "id")
	if err != nil {
		return store.NoteBlock{}, err
	}
	if !ok || id == uuid.Nil {
		return store.NoteBlock{}, errors.New("id is required")
	}

	blockType, ok, err := getStringField(fields, "blockType")
	if err != nil {
		return store.NoteBlock{}, err
	}
	if !ok {
		return store.NoteBlock{}, errors.New("blockType is required")
	}
	if err := validateBlockType(blockType); err != nil {
		return store.NoteBlock{}, err
	}

	position, ok, err := getFloatField(fields, "position")
	if err != nil {
		return store.NoteBlock{}, err
	}
	if !ok {
		return store.NoteBlock{}, errors.New("position is required")
	}
	if position < 0 {
		return store.NoteBlock{}, errors.New("position must be greater than or equal to zero")
	}

	textContent, ok, err := getStringField(fields, "textContent")
	if err != nil {
		return store.NoteBlock{}, err
	}
	if !ok {
		return store.NoteBlock{}, errors.New("textContent is required")
	}

	blockProperties, ok, err := getObjectField(fields, "blockProperties")
	if err != nil {
		return store.NoteBlock{}, err
	}
	if !ok {
		blockProperties = json.RawMessage(`{}`)
	}

	return store.NoteBlock{
		ID:              id,
		NoteID:          noteID,
		BlockType:       blockType,
		TextContent:     textContent,
		Position:        strconv.FormatFloat(position, 'f', -1, 64),
		BlockProperties: blockProperties,
	}, nil
}

// mutateNote applies update/delete operations for existing notes.
func (s *Service) mutateNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedDTO, *RejectedDTO, error) {
	if document == nil {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if document.Note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, document.Note.CurrentVersion)
		return nil, &rejected, nil
	}

	switch op.OperationType {
	case OperationModifyNoteProperty:
		notePropertiesRaw, ok, err := getObjectField(fields, "noteProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if !ok {
			rejected := rejectedInvalid(op, "modify_note_property must include at least one property.")
			return nil, &rejected, nil
		}
		changes, err := decodeObjectFields(notePropertiesRaw, "noteProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if len(changes) == 0 {
			rejected := rejectedInvalid(op, "modify_note_property must include at least one property.")
			return nil, &rejected, nil
		}
		noteProperties, err := mergeJSONProperties(document.Note.NoteProperties, changes, "noteProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		document.Note.NoteProperties = noteProperties
		return s.acceptOperationChange(ctx, tx, ownerID, deviceID, document, op, nil)

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
		title, err := applyTextDelta(document.Note.Title, textChange.Text, textChange.TextOperation, textChange.Index)
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
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, &op.NoteID, blockID, baseVersion, baseVersion+1)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createBlock applies create_block to the in-memory note document.
func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedDTO, *RejectedDTO, error) {
	block, err := decodeBlockPayload(fields, op.NoteID)
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
func (s *Service) mutateBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, document *store.NoteDocument, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedDTO, *RejectedDTO, error) {
	var (
		changedProperties  map[string]json.RawMessage
		textChange         TextChange
		hasBlockProperties bool
		hasText            bool
		blockID            uuid.UUID
	)

	blockID, ok, err := getUUIDField(fields, "id")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok || blockID == uuid.Nil {
		rejected := rejectedInvalid(op, "id is required.")
		return nil, &rejected, nil
	}

	if op.OperationType == OperationModifyBlock {
		var blockPropertiesRaw json.RawMessage
		blockPropertiesRaw, hasBlockProperties = fields["blockProperties"]
		if hasBlockProperties && !isJSONNull(blockPropertiesRaw) {
			var err error
			blockPropertiesRaw, _, err = getObjectField(fields, "blockProperties")
			if err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
			changedProperties, err = decodeObjectFields(blockPropertiesRaw, "blockProperties")
			if err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
		}
		textDeltaRaw, hasTextDelta, err := getObjectField(fields, "textDelta")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		if hasTextDelta {
			textChange, err = decodeTextChange(textDeltaRaw, "textDelta")
			if err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
			hasText = true
		}
		if !hasBlockProperties && !hasText {
			rejected := rejectedInvalid(op, "modify_block must include blockProperties or textDelta.")
			return nil, &rejected, nil
		}
	}

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
	if hasBlockProperties && len(changedProperties) > 0 {
		blockProperties, err := mergeJSONProperties(block.BlockProperties, changedProperties, "blockProperties")
		if err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		block.BlockProperties = blockProperties
	}
	if hasText {
		text, err := applyTextDelta(block.TextContent, textChange.Text, textChange.TextOperation, textChange.Index)
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
func acceptedFromChange(change store.NoteChange) AcceptedDTO {
	return AcceptedDTO{
		NoteID:            change.NoteID,
		ServerNoteVersion: change.ResultingNoteVersion,
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
			ID:              block.ID,
			NoteID:          block.NoteID,
			BlockType:       block.BlockType,
			TextContent:     block.TextContent,
			Position:        block.Position,
			BlockProperties: store.NormalizeJSON(block.BlockProperties),
			CreatedAt:       block.CreatedAt,
			UpdatedAt:       block.UpdatedAt,
			DeletedAt:       store.TimePtr(block.DeletedAt),
		})
	}
	return NoteSnapshot{
		ID:             doc.Note.ID,
		OwnerID:        doc.Note.OwnerID,
		Title:          doc.Note.Title,
		NoteProperties: store.NormalizeJSON(doc.Note.NoteProperties),
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

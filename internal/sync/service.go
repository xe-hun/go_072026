package syncapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"notes-server/internal/config"
	"notes-server/internal/httpapi"
	"notes-server/internal/store"
)

type Service struct {
	store                   *store.Store
	logger                  *slog.Logger
	maxOperations           int
	defaultPullLimit        int32
	maxPullLimit            int32
	snapshotChangeThreshold int64
	snapshotByteThreshold   int64
}

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

		for _, op := range req.Operations {
			accepted, rejected, err := s.applyOperation(ctx, tx, ownerID, req.DeviceID, op)
			if err != nil {
				return err
			}
			if rejected != nil {
				resp.Rejected = append(resp.Rejected, *rejected)
				continue
			}
			if accepted != nil {
				resp.Accepted = append(resp.Accepted, *accepted)
			}
		}

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

func (s *Service) applyOperation(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation) (*AcceptedOperation, *RejectedOperation, error) {
	if processed, err := tx.FindProcessedOperation(ctx, deviceID, op.OperationID); err == nil {
		accepted := acceptedFromChange(processed)
		return &accepted, nil, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}

	if err := validateOperationShape(op); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}

	fields, err := decodeFields(op.ChangeData)
	if err != nil {
		rejected := rejectedInvalid(op, "changeData is invalid.")
		return nil, &rejected, nil
	}

	switch op.OperationType {
	case OperationCreateNote:
		return s.createNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationUpdateNote, OperationDeleteNote, OperationRestoreNote:
		return s.mutateNote(ctx, tx, ownerID, deviceID, op, fields)
	case OperationCreateBlock:
		return s.createBlock(ctx, tx, ownerID, deviceID, op, fields)
	case OperationUpdateBlock, OperationMoveBlock, OperationDeleteBlock, OperationRestoreBlock:
		return s.mutateBlock(ctx, tx, ownerID, deviceID, op, fields)
	default:
		rejected := rejectedInvalid(op, "operationType is unsupported.")
		return nil, &rejected, nil
	}
}

func (s *Service) createNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	if op.BaseNoteVersion != 0 {
		rejected := rejectedConflict(op, 0, nil)
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
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, 0, created.CurrentVersion, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, created.ID, ownerID, created.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) mutateNote(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, note.CurrentVersion, nil)
		return nil, &rejected, nil
	}

	baseVersion := note.CurrentVersion
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
	case OperationDeleteNote:
		note.DeletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	case OperationRestoreNote:
		note.DeletedAt = pgtype.Timestamptz{}
	}

	updated, err := tx.UpdateNoteState(ctx, note)
	if err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, baseVersion, updated.CurrentVersion, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, updated.ID, ownerID, updated.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, note.CurrentVersion, nil)
		return nil, &rejected, nil
	}

	blockType, ok, err := getStringField(fields, "blockType")
	if err != nil || !ok {
		rejected := rejectedInvalid(op, "blockType is required.")
		return nil, &rejected, nil
	}
	if err := validateBlockType(blockType); err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	position, ok, err := getStringField(fields, "position")
	if err != nil || !ok || position == "" {
		rejected := rejectedInvalid(op, "position is required.")
		return nil, &rejected, nil
	}
	text, ok, err := getStringField(fields, "textContent")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		text = ""
	}
	properties, ok, err := getObjectField(fields, "properties")
	if err != nil {
		rejected := rejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if !ok {
		properties = json.RawMessage(`{}`)
	}

	baseNoteVersion := note.CurrentVersion
	note.CurrentVersion++
	if _, err := tx.UpdateNoteState(ctx, note); err != nil {
		return nil, nil, err
	}

	block, err := tx.CreateBlock(ctx, store.NoteBlock{
		ID:             *op.BlockID,
		NoteID:         note.ID,
		BlockType:      blockType,
		TextContent:    text,
		Position:       position,
		Properties:     properties,
		CurrentVersion: 1,
	})
	if err != nil {
		return nil, nil, err
	}
	resultBlockVersion := block.CurrentVersion
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, op.BlockID, baseNoteVersion, note.CurrentVersion, nil, &resultBlockVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, note.ID, ownerID, note.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) mutateBlock(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, fields map[string]json.RawMessage) (*AcceptedOperation, *RejectedOperation, error) {
	note, err := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := rejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if note.CurrentVersion != op.BaseNoteVersion {
		rejected := rejectedConflict(op, note.CurrentVersion, nil)
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
	if op.BaseBlockVersion == nil || block.CurrentVersion != *op.BaseBlockVersion {
		serverBlockVersion := block.CurrentVersion
		rejected := rejectedConflict(op, note.CurrentVersion, &serverBlockVersion)
		return nil, &rejected, nil
	}

	baseNoteVersion := note.CurrentVersion
	baseBlockVersion := block.CurrentVersion
	note.CurrentVersion++
	block.CurrentVersion++

	switch op.OperationType {
	case OperationUpdateBlock:
		if blockType, ok, err := getStringField(fields, "blockType"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			if err := validateBlockType(blockType); err != nil {
				rejected := rejectedInvalid(op, err.Error())
				return nil, &rejected, nil
			}
			block.BlockType = blockType
		}
		if text, ok, err := getStringField(fields, "textContent"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			block.TextContent = text
		}
		if properties, ok, err := getObjectField(fields, "properties"); err != nil {
			rejected := rejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		} else if ok {
			block.Properties = properties
		}
	case OperationMoveBlock:
		position, ok, err := getStringField(fields, "position")
		if err != nil || !ok || position == "" {
			rejected := rejectedInvalid(op, "position is required.")
			return nil, &rejected, nil
		}
		block.Position = position
	case OperationDeleteBlock:
		block.DeletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	case OperationRestoreBlock:
		block.DeletedAt = pgtype.Timestamptz{}
	}

	if _, err := tx.UpdateNoteState(ctx, note); err != nil {
		return nil, nil, err
	}
	updatedBlock, err := tx.UpdateBlockState(ctx, block)
	if err != nil {
		return nil, nil, err
	}
	resultBlockVersion := updatedBlock.CurrentVersion
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, op.BlockID, baseNoteVersion, note.CurrentVersion, &baseBlockVersion, &resultBlockVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := s.enqueueSnapshotIfNeeded(ctx, tx, note.ID, ownerID, note.CurrentVersion); err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) insertChange(ctx context.Context, tx *store.Store, ownerID, deviceID uuid.UUID, op ClientOperation, blockID *uuid.UUID, baseNoteVersion, resultingNoteVersion int64, baseBlockVersion, resultingBlockVersion *int64) (store.NoteChange, error) {
	return tx.InsertNoteChange(ctx, store.InsertNoteChangeParams{
		ID:                    uuid.New(),
		OwnerID:               ownerID,
		NoteID:                op.NoteID,
		BlockID:               blockID,
		DeviceID:              deviceID,
		ClientOperationID:     op.OperationID,
		EntityType:            op.EntityType,
		OperationType:         op.OperationType,
		BaseNoteVersion:       baseNoteVersion,
		ResultingNoteVersion:  resultingNoteVersion,
		BaseBlockVersion:      baseBlockVersion,
		ResultingBlockVersion: resultingBlockVersion,
		ChangeFormat:          normalizeChangeFormat(op.ChangeFormat),
		SchemaVersion:         1,
		ChangeData:            op.ChangeData,
	})
}

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

func acceptedFromChange(change store.NoteChange) AcceptedOperation {
	return AcceptedOperation{
		OperationID:  change.ClientOperationID,
		NoteID:       change.NoteID,
		BlockID:      store.UUIDPtr(change.BlockID),
		NoteVersion:  change.ResultingNoteVersion,
		BlockVersion: store.Int64Ptr(change.ResultingBlockVersion),
		Sequence:     change.GlobalSequence,
	}
}

func mapPulledChange(change store.NoteChange) PulledChange {
	return PulledChange{
		ID:                    change.ID,
		OperationID:           change.ClientOperationID,
		NoteID:                change.NoteID,
		BlockID:               store.UUIDPtr(change.BlockID),
		DeviceID:              change.DeviceID,
		EntityType:            change.EntityType,
		OperationType:         change.OperationType,
		BaseNoteVersion:       change.BaseNoteVersion,
		ResultingNoteVersion:  change.ResultingNoteVersion,
		BaseBlockVersion:      store.Int64Ptr(change.BaseBlockVersion),
		ResultingBlockVersion: store.Int64Ptr(change.ResultingBlockVersion),
		ChangeFormat:          change.ChangeFormat,
		SchemaVersion:         change.SchemaVersion,
		ChangeData:            store.NormalizeJSON(change.ChangeData),
		Sequence:              change.GlobalSequence,
		CreatedAt:             change.CreatedAt,
	}
}

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

package services

import (
	"context"
	"errors"
	"github.com/google/uuid"
	constants "notes-server/internal"
	"notes-server/internal/store"
	. "notes-server/internal/sync/models"
	. "notes-server/internal/sync/operations"
)

// operation implementation.
func (s *Service) applyOperation(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*noteBatchState, *AcceptedDTO, *RejectedDTO, bool, error) {
	if processed, err := tx.FindProcessedOperation(ctx, deviceID, op.OperationID); err == nil {
		// A retry returns the original accepted result and does not mutate state
		// again.
		if op.NoteID != uuid.Nil {
			if err := state.ensureNote(ctx, tx, ownerID, op.NoteID); err != nil {
				return state, nil, nil, false, err
			}
		}
		accepted := acceptedFromChange(processed)
		return state, &accepted, nil, false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return state, nil, nil, false, err
	}

	var operation Operation
	if err := operation.FromRequest(op); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return state, nil, &rejected, false, nil
	}
	switch op.OperationType {
	case OperationCreateNote:
		accepted, rejected, err := s.createNote(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationDeleteNote:
		accepted, rejected, err := s.deleteNote(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationModifyNoteProperty:
		accepted, rejected, err := s.modifyNoteProperty(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationModifyNoteTitle:
		accepted, rejected, err := s.modifyNoteTitle(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationCreateBlock:
		accepted, rejected, err := s.createBlock(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationDeleteBlock:
		accepted, rejected, err := s.deleteBlock(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationModifyBlock:
		accepted, rejected, err := s.modifyBlock(ctx, tx, ownerID, deviceID, state, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationCreateCategory:
		accepted, rejected, err := s.createCategory(ctx, tx, ownerID, deviceID, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationDeleteCategory:
		accepted, rejected, err := s.deleteCategory(ctx, tx, ownerID, deviceID, op)
		return state, accepted, rejected, rejected == nil, err
	case OperationModifyCategory:
		accepted, rejected, err := s.modifyCategory(ctx, tx, ownerID, deviceID, op)
		return state, accepted, rejected, rejected == nil, err
	default:
		rejected := RejectedInvalid(op, constants.RequestKeyOperationType+" is unsupported.")
		return state, nil, &rejected, false, nil
	}
}

func (s *Service) createCategory(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var category Category
	if err := category.FromRequest(op.ChangeData, true); err != nil {
		rejected := RejectedInvalid(op, err.Error())
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

func (s *Service) deleteCategory(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var category Category
	if err := category.FromRequest(op.ChangeData, false); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := tx.LockCategoryForOwner(ctx, category.ID, ownerID); errors.Is(err, store.ErrNotFound) {
		rejected := RejectedNotFound(op, "Category not found.")
		return nil, &rejected, nil
	} else if err != nil {
		return nil, nil, err
	}
	if err := tx.DeleteCategory(ctx, category.ID, ownerID); err != nil {
		return nil, nil, err
	}
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, nil, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

func (s *Service) modifyCategory(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var category Category
	if err := category.FromRequest(op.ChangeData, true); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	err := tx.LockCategoryForOwner(ctx, category.ID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := RejectedNotFound(op, "Category not found.")
		return nil, &rejected, nil
	} else if err != nil {
		return nil, nil, err
	}

	if err := tx.UpdateCategory(ctx, category.ID, ownerID, category.Name); err != nil {
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

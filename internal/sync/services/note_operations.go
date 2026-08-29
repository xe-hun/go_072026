package services

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"notes-server/internal/store"
	. "notes-server/internal/sync/models"
	. "notes-server/internal/sync/operations"
)

// results in note version 1.
func (s *Service) createNote(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	if op.BaseNoteVersion != 0 {
		rejected := RejectedConflict(op, 0)
		return nil, &rejected, nil
	}
	if state.noteLoaded && state.note != nil {
		rejected := RejectedConflict(op, state.note.CurrentVersion)
		return nil, &rejected, nil
	}

	var model NoteModel
	if err := model.FromRequest(op, ownerID); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	document, err := model.Entity()
	if err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := tx.CreateNote(ctx, document.Note); err != nil {
		if store.IsUniqueViolation(err) {
			rejected := RejectedConflict(op, 0)
			return nil, &rejected, nil
		}
		return nil, nil, err
	}
	state.note = &document.Note
	state.noteLoaded = true
	for _, block := range document.Blocks {
		if err := tx.CreateBlock(ctx, block); err != nil {
			if store.IsUniqueViolation(err) {
				rejected := RejectedInvalid(op, "block already exists.")
				return nil, &rejected, nil
			}
			return nil, nil, err
		}
		created := block
		state.blocks[created.ID] = &created
	}
	accepted, _, err := s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, nil)
	if err != nil {
		return nil, nil, err
	}
	return accepted, nil, nil
}

func (s *Service) existingNote(ctx context.Context, tx *store.Store, ownerID uuid.UUID, state *noteBatchState, op ClientOperation) (*RejectedDTO, error) {
	if err := state.ensureNote(ctx, tx, ownerID, op.NoteID); err != nil {
		return nil, err
	}
	if state.note == nil {
		rejected := RejectedNotFound(op, "Note not found.")
		return &rejected, nil
	}
	if state.note.CurrentVersion != op.BaseNoteVersion {
		rejected := RejectedConflict(op, state.note.CurrentVersion)
		return &rejected, nil
	}
	return nil, nil
}

func (s *Service) deleteNote(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	deleted, err := tx.DeleteNote(ctx, op.NoteID, ownerID, op.BaseNoteVersion)
	if errors.Is(err, store.ErrNotFound) {
		current, lookupErr := tx.GetNoteForOwnerForUpdate(ctx, op.NoteID, ownerID)
		if errors.Is(lookupErr, store.ErrNotFound) {
			rejected := RejectedNotFound(op, "Note not found.")
			return nil, &rejected, nil
		}
		if lookupErr != nil {
			return nil, nil, lookupErr
		}
		rejected := RejectedConflict(op, current.CurrentVersion)
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if state.note == nil {
		state.note = &deleted
	} else {
		// Preserve earlier in-batch note edits while adopting the persisted
		// deletion timestamps.
		state.note.DeletedAt = deleted.DeletedAt
		state.note.UpdatedAt = deleted.UpdatedAt
	}
	state.noteLoaded = true
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, nil)
}

func (s *Service) modifyNoteProperty(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var mutation NoteMutation
	if err := mutation.FromRequest(op.ChangeData, OperationModifyNoteProperty); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if rejected, err := s.existingNote(ctx, tx, ownerID, state, op); err != nil || rejected != nil {
		return nil, rejected, err
	}
	state.note.NoteProperties = mutation.NoteProperties
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, nil)
}

func (s *Service) modifyNoteTitle(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var mutation NoteMutation
	if err := mutation.FromRequest(op.ChangeData, OperationModifyNoteTitle); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if rejected, err := s.existingNote(ctx, tx, ownerID, state, op); err != nil || rejected != nil {
		return nil, rejected, err
	}
	title, err := applyTextDelta(state.note.Title, mutation.TextDelta)
	if err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	state.note.Title = title
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, nil)
}

func (s *Service) acceptOperationChange(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, note *store.Note, op ClientOperation, blockID *uuid.UUID) (*AcceptedDTO, *RejectedDTO, error) {
	baseVersion := note.CurrentVersion
	change, err := s.insertChange(ctx, tx, ownerID, deviceID, op, blockID, baseVersion, baseVersion+1)
	if err != nil {
		return nil, nil, err
	}
	accepted := acceptedFromChange(change)
	return &accepted, nil, nil
}

// createBlock writes a new block directly and keeps only its state in the

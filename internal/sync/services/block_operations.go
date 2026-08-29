package services

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"notes-server/internal/store"
	. "notes-server/internal/sync/models"
	. "notes-server/internal/sync/operations"
	"time"
)

// batch when a later operation targets it.
func (s *Service) createBlock(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var blockModel BlockModel
	if err := blockModel.FromCreateOperation(op.ChangeData); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	block, err := blockModel.Entity(op.NoteID)
	if err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	if err := state.ensureNote(ctx, tx, ownerID, op.NoteID); err != nil {
		return nil, nil, err
	}
	if state.note == nil {
		rejected := RejectedNotFound(op, "Note not found.")
		return nil, &rejected, nil
	}
	if state.note.CurrentVersion != op.BaseNoteVersion {
		rejected := RejectedConflict(op, state.note.CurrentVersion)
		return nil, &rejected, nil
	}
	if _, exists := state.blocks[block.ID]; exists {
		rejected := RejectedInvalid(op, "block already exists.")
		return nil, &rejected, nil
	}
	if err := tx.CreateBlock(ctx, block); err != nil {
		if store.IsUniqueViolation(err) {
			rejected := RejectedInvalid(op, "block already exists.")
			return nil, &rejected, nil
		}
		return nil, nil, err
	}
	state.blocks[block.ID] = &block
	delete(state.missingBlocks, block.ID)
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, &block.ID)
}

func (s *Service) deleteBlock(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var mutation BlockMutation
	if err := mutation.FromRequest(op.ChangeData, OperationDeleteBlock); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	blockID := mutation.ID
	if rejected, err := s.existingNote(ctx, tx, ownerID, state, op); err != nil || rejected != nil {
		return nil, rejected, err
	}
	block, exists := state.blocks[blockID]
	if exists {
		block.DeletedAt.Valid = true
		block.DeletedAt.Time = time.Now().UTC()
		if err := tx.UpdateBlockState(ctx, *block); err != nil {
			return nil, nil, err
		}
		delete(state.dirtyBlocks, blockID)
	} else {
		deleted, err := tx.DeleteBlock(ctx, blockID, op.NoteID)
		if errors.Is(err, store.ErrNotFound) {
			rejected := RejectedNotFound(op, "Block not found.")
			return nil, &rejected, nil
		}
		if err != nil {
			return nil, nil, err
		}
		state.blocks[blockID] = &deleted
	}
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, &blockID)
}

func (s *Service) modifyBlock(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, state *noteBatchState, op ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	var mutation BlockMutation
	if err := mutation.FromRequest(op.ChangeData, OperationModifyBlock); err != nil {
		rejected := RejectedInvalid(op, err.Error())
		return nil, &rejected, nil
	}
	blockID := mutation.ID
	if rejected, err := s.existingNote(ctx, tx, ownerID, state, op); err != nil || rejected != nil {
		return nil, rejected, err
	}
	block, err := state.ensureBlock(ctx, tx, op.NoteID, blockID)
	if errors.Is(err, store.ErrNotFound) {
		rejected := RejectedNotFound(op, "Block not found.")
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if mutation.HasProperties {
		block.BlockProperties = mutation.BlockProperties
	}
	if mutation.HasTextDelta {
		text, err := applyTextDelta(block.TextContent, mutation.TextDelta)
		if err != nil {
			rejected := RejectedInvalid(op, err.Error())
			return nil, &rejected, nil
		}
		block.TextContent = text
	}
	state.dirtyBlocks[blockID] = true
	return s.acceptOperationChange(ctx, tx, ownerID, deviceID, state.note, op, &blockID)
}

// applyTextDelta replaces deleteCount runes at index with insertText.
func applyTextDelta(text string, delta TextDelta) (string, error) {
	textRunes := []rune(text)
	if delta.Index < 0 || delta.Index > len(textRunes) {
		return "", errors.New("Text delta index is out of range.")
	}
	if delta.DeleteCount < 0 {
		return "", errors.New("Text delta delete count is invalid.")
	}
	if delta.DeleteCount > len(textRunes)-delta.Index {
		return "", errors.New("Text delta delete range is out of range.")
	}

	insertRunes := []rune(delta.InsertText)
	end := delta.Index + delta.DeleteCount
	result := make([]rune, 0, len(textRunes)-delta.DeleteCount+len(insertRunes))
	result = append(result, textRunes[:delta.Index]...)
	result = append(result, insertRunes...)
	result = append(result, textRunes[end:]...)
	return string(result), nil
}

package syncapi

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"notes-server/internal/store"
	"sort"
)

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

// noteBatchState keeps only the note row and block rows touched by a batch.
// It deliberately does not contain the complete note document.
type noteBatchState struct {
	note          *store.Note
	noteLoaded    bool
	blocks        map[uuid.UUID]*store.NoteBlock
	missingBlocks map[uuid.UUID]bool
	dirtyBlocks   map[uuid.UUID]bool
}

func newNoteBatchState() *noteBatchState {
	return &noteBatchState{
		blocks:        make(map[uuid.UUID]*store.NoteBlock),
		missingBlocks: make(map[uuid.UUID]bool),
		dirtyBlocks:   make(map[uuid.UUID]bool),
	}
}

func (b *noteBatchState) ensureNote(ctx context.Context, tx *store.Store, ownerID, noteID uuid.UUID) error {
	if b.noteLoaded {
		return nil
	}
	note, err := tx.GetNoteForOwnerForUpdate(ctx, noteID, ownerID)
	if errors.Is(err, store.ErrNotFound) {
		b.noteLoaded = true
		return nil
	}
	if err != nil {
		return err
	}
	b.note = &note
	b.noteLoaded = true
	return nil
}

func (b *noteBatchState) ensureBlock(ctx context.Context, tx *store.Store, noteID, blockID uuid.UUID) (*store.NoteBlock, error) {
	if block, ok := b.blocks[blockID]; ok {
		return block, nil
	}
	if b.missingBlocks[blockID] {
		return nil, store.ErrNotFound
	}
	block, err := tx.GetBlockForNoteForUpdate(ctx, blockID, noteID)
	if errors.Is(err, store.ErrNotFound) {
		b.missingBlocks[blockID] = true
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b.blocks[blockID] = &block
	return b.blocks[blockID], nil
}

// applyOperationBatch applies all operations for one note under a savepoint.
// Direct create/delete operations are written immediately; targeted edits are
// kept in noteBatchState until the batch has been accepted.
func (s *Service) applyOperationBatch(ctx context.Context, tx *store.Store, ownerID uuid.UUID, deviceID string, operations []ClientOperation) (*AcceptedDTO, *RejectedDTO, error) {
	if len(operations) == 0 {
		return nil, nil, nil
	}
	operations = sortedOperations(operations)

	state := newNoteBatchState()

	var failed RejectedDTO
	changed := false
	var latestCursor int64

	err := tx.WithSavepoint(ctx, func(batchTx *store.Store) error {
		for _, op := range operations {
			var accepted *AcceptedDTO
			var rejected *RejectedDTO
			var operationChanged bool
			var err error
			state, accepted, rejected, operationChanged, err = s.applyOperation(ctx, batchTx, ownerID, deviceID, state, op)
			if err != nil {
				return err
			}
			changed = changed || operationChanged
			if operationChanged && accepted != nil && accepted.globalSequence > latestCursor {
				latestCursor = accepted.globalSequence
			}

			if rejected != nil {
				failed = *rejected
				return errOperationBatchRejected
			}
		}
		return nil
	})
	if errors.Is(err, errOperationBatchRejected) {
		var document *store.NoteDocument
		if operations[0].NoteID != uuid.Nil {
			loaded, loadErr := tx.GetNoteDocumentForOwnerForUpdate(ctx, operations[0].NoteID, ownerID)
			if loadErr == nil {
				document = &loaded
			} else if !errors.Is(loadErr, store.ErrNotFound) {
				return nil, nil, loadErr
			}
		}
		rejected := rejectedBatchDTO(operations, failed, document)
		return nil, &rejected, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if changed && state.note != nil {
		state.note.CurrentVersion++
		if err := tx.UpdateNoteState(ctx, *state.note); err != nil {
			return nil, nil, err
		}
		for blockID := range state.dirtyBlocks {
			if err := tx.UpdateBlockState(ctx, *state.blocks[blockID]); err != nil {
				return nil, nil, err
			}
		}
		if err := s.enqueueSnapshotIfNeeded(ctx, tx, state.note.ID, ownerID, state.note.CurrentVersion); err != nil {
			return nil, nil, err
		}
	}

	noteVersion := int64(0)
	if state.note != nil {
		noteVersion = state.note.CurrentVersion
	}
	accepted := AcceptedDTO{
		NoteID:            operations[0].NoteID,
		ServerNoteVersion: noteVersion,
		globalSequence:    latestCursor,
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

// applyOperation handles one operation from a sync batch. It first checks the
// idempotency table, then validates shape and dispatches to the concrete

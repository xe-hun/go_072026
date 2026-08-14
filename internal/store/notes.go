package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	db "notes-server/db/generated"
)

// GetNoteForOwner fetches a note only when owner_id matches the authenticated
// user.
func (s *Store) GetNoteForOwner(ctx context.Context, noteID, ownerID uuid.UUID) (Note, error) {
	note, err := s.q.GetNoteForOwner(ctx, db.GetNoteForOwnerParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBNote(note), mapNoRows(err)
}

// GetNoteForOwnerForUpdate locks the note row. Sync uses this to serialize
// conflicting writes to the same note.
func (s *Store) GetNoteForOwnerForUpdate(ctx context.Context, noteID, ownerID uuid.UUID) (Note, error) {
	note, err := s.q.GetNoteForOwnerForUpdate(ctx, db.GetNoteForOwnerForUpdateParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return fromDBNote(note), mapNoRows(err)
}

// ListBlocksForNote returns all blocks for a note in stable position order,
// including tombstones.
func (s *Store) ListBlocksForNote(ctx context.Context, noteID uuid.UUID) ([]NoteBlock, error) {
	rows, err := s.q.ListBlocksForNote(ctx, pgUUID(noteID))
	if err != nil {
		return nil, err
	}
	blocks := make([]NoteBlock, 0, len(rows))
	for _, row := range rows {
		blocks = append(blocks, fromDBBlock(row))
	}
	return blocks, nil
}

// GetBlockForNoteForUpdate locks a block row during block mutation.
func (s *Store) GetBlockForNoteForUpdate(ctx context.Context, noteID, blockID uuid.UUID) (NoteBlock, error) {
	block, err := s.q.GetBlockForNoteForUpdate(ctx, db.GetBlockForNoteForUpdateParams{
		ID:     pgUUID(blockID),
		NoteID: pgUUID(noteID),
	})
	return fromDBBlock(block), mapNoRows(err)
}

// CreateNote inserts current note state. Sync also inserts the corresponding
// note_changes row in the same transaction.
func (s *Store) CreateNote(ctx context.Context, note Note) (Note, error) {
	created, err := s.q.CreateNote(ctx, db.CreateNoteParams{
		ID:             pgUUID(note.ID),
		OwnerID:        pgUUID(note.OwnerID),
		CategoryID:     note.CategoryID,
		Title:          note.Title,
		Metadata:       []byte(NormalizeJSON(note.Metadata)),
		CurrentVersion: note.CurrentVersion,
	})
	return fromDBNote(created), err
}

// UpdateNoteState writes note current state after the service has already
// validated versions and incremented CurrentVersion.
func (s *Store) UpdateNoteState(ctx context.Context, note Note) (Note, error) {
	updated, err := s.q.UpdateNoteState(ctx, db.UpdateNoteStateParams{
		ID:             pgUUID(note.ID),
		OwnerID:        pgUUID(note.OwnerID),
		CategoryID:     note.CategoryID,
		Title:          note.Title,
		Metadata:       []byte(NormalizeJSON(note.Metadata)),
		CurrentVersion: note.CurrentVersion,
		DeletedAt:      note.DeletedAt,
	})
	return fromDBNote(updated), mapNoRows(err)
}

// CreateBlock inserts current block state.
func (s *Store) CreateBlock(ctx context.Context, block NoteBlock) (NoteBlock, error) {
	created, err := s.q.CreateBlock(ctx, db.CreateBlockParams{
		ID:          pgUUID(block.ID),
		NoteID:      pgUUID(block.NoteID),
		BlockType:   block.BlockType,
		TextContent: block.TextContent,
		Position:    block.Position,
		Properties:  []byte(NormalizeJSON(block.Properties)),
	})
	return fromDBBlock(created), err
}

// UpdateBlockState writes block current state after the service has validated
// the parent note version.
func (s *Store) UpdateBlockState(ctx context.Context, block NoteBlock) (NoteBlock, error) {
	updated, err := s.q.UpdateBlockState(ctx, db.UpdateBlockStateParams{
		ID:          pgUUID(block.ID),
		NoteID:      pgUUID(block.NoteID),
		BlockType:   block.BlockType,
		TextContent: block.TextContent,
		Position:    block.Position,
		Properties:  []byte(NormalizeJSON(block.Properties)),
		DeletedAt:   block.DeletedAt,
	})
	return fromDBBlock(updated), mapNoRows(err)
}

// GetNoteDocument fetches a note and its blocks for read endpoints and snapshot
// creation.
func (s *Store) GetNoteDocument(ctx context.Context, noteID, ownerID uuid.UUID) (NoteDocument, error) {
	note, err := s.GetNoteForOwner(ctx, noteID, ownerID)
	if err != nil {
		return NoteDocument{}, err
	}
	blocks, err := s.ListBlocksForNote(ctx, noteID)
	if err != nil {
		return NoteDocument{}, err
	}
	return NoteDocument{Note: note, Blocks: blocks}, nil
}

// RawObjectOrEmpty is a small JSON helper for callers that want empty JSONB
// objects instead of nil raw messages.
func RawObjectOrEmpty(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}

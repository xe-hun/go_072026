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

// GetNoteDocumentForOwnerForUpdate locks and loads one complete note document
// for an atomic operation batch.
func (s *Store) GetNoteDocumentForOwnerForUpdate(ctx context.Context, noteID, ownerID uuid.UUID) (NoteDocument, error) {
	note, err := s.q.GetNoteForOwnerForUpdate(ctx, db.GetNoteForOwnerForUpdateParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	if err != nil {
		return NoteDocument{}, mapNoRows(err)
	}
	blocks, err := s.ListBlocksForNote(ctx, noteID)
	if err != nil {
		return NoteDocument{}, err
	}
	return NoteDocument{Note: fromDBNote(note), Blocks: blocks}, nil
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

// CreateNote inserts a new note.
func (s *Store) CreateNote(ctx context.Context, note Note) error {
	return s.q.CreateNote(ctx, db.CreateNoteParams{
		ID:             pgUUID(note.ID),
		OwnerID:        pgUUID(note.OwnerID),
		Title:          note.Title,
		NoteProperties: []byte(NormalizeJSON(note.NoteProperties)),
		CurrentVersion: note.CurrentVersion,
	})
}

// UpdateNoteState writes the complete note state after a successful batch.
func (s *Store) UpdateNoteState(ctx context.Context, note Note) error {
	return s.q.UpdateNoteState(ctx, db.UpdateNoteStateParams{
		ID:             pgUUID(note.ID),
		OwnerID:        pgUUID(note.OwnerID),
		Title:          note.Title,
		NoteProperties: []byte(NormalizeJSON(note.NoteProperties)),
		CurrentVersion: note.CurrentVersion,
		DeletedAt:      note.DeletedAt,
	})
}

// CreateBlock inserts a new block.
func (s *Store) CreateBlock(ctx context.Context, block NoteBlock) error {
	return s.q.CreateBlock(ctx, db.CreateBlockParams{
		ID:              pgUUID(block.ID),
		NoteID:          pgUUID(block.NoteID),
		BlockType:       block.BlockType,
		TextContent:     block.TextContent,
		Position:        block.Position,
		BlockProperties: []byte(NormalizeJSON(block.BlockProperties)),
	})
}

// UpdateBlockState writes an existing block after a successful batch.
func (s *Store) UpdateBlockState(ctx context.Context, block NoteBlock) error {
	return s.q.UpdateBlockState(ctx, db.UpdateBlockStateParams{
		ID:              pgUUID(block.ID),
		NoteID:          pgUUID(block.NoteID),
		BlockType:       block.BlockType,
		TextContent:     block.TextContent,
		Position:        block.Position,
		BlockProperties: []byte(NormalizeJSON(block.BlockProperties)),
		DeletedAt:       block.DeletedAt,
	})
}

// SaveNoteDocument persists a complete note document once its operation batch
// has succeeded. A zero CreatedAt marks a note or block created in memory.
func (s *Store) SaveNoteDocument(ctx context.Context, document NoteDocument) error {
	if document.Note.CreatedAt.IsZero() {
		if err := s.CreateNote(ctx, document.Note); err != nil {
			return err
		}
	} else if err := s.UpdateNoteState(ctx, document.Note); err != nil {
		return err
	}

	for _, block := range document.Blocks {
		if block.CreatedAt.IsZero() {
			if err := s.CreateBlock(ctx, block); err != nil {
				return err
			}
			continue
		}
		if err := s.UpdateBlockState(ctx, block); err != nil {
			return err
		}
	}
	return nil
}

// GetNoteDocument fetches a note and its blocks for read endpoints and
// snapshot creation.
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

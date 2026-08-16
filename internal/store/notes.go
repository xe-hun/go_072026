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

// GetNoteVersionForOwnerForUpdate locks a note while reading only its version.
func (s *Store) GetNoteVersionForOwnerForUpdate(ctx context.Context, noteID, ownerID uuid.UUID) (int64, error) {
	version, err := s.q.GetNoteVersionForOwnerForUpdate(ctx, db.GetNoteVersionForOwnerForUpdateParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return version, mapNoRows(err)
}

// GetNoteMetadataForOwnerForUpdate locks a note while reading only metadata
// and its version.
func (s *Store) GetNoteMetadataForOwnerForUpdate(ctx context.Context, noteID, ownerID uuid.UUID) (json.RawMessage, int64, error) {
	value, err := s.q.GetNoteMetadataForOwnerForUpdate(ctx, db.GetNoteMetadataForOwnerForUpdateParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return NormalizeJSON(json.RawMessage(value.Metadata)), value.CurrentVersion, mapNoRows(err)
}

// GetNoteTitleForOwnerForUpdate locks a note while reading only its title and
// version.
func (s *Store) GetNoteTitleForOwnerForUpdate(ctx context.Context, noteID, ownerID uuid.UUID) (string, int64, error) {
	value, err := s.q.GetNoteTitleForOwnerForUpdate(ctx, db.GetNoteTitleForOwnerForUpdateParams{
		ID:      pgUUID(noteID),
		OwnerID: pgUUID(ownerID),
	})
	return value.Title, value.CurrentVersion, mapNoRows(err)
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

// LockBlockForUpdate locks a block row without loading its state.
func (s *Store) LockBlockForUpdate(ctx context.Context, noteID, blockID uuid.UUID, position string) error {
	_, err := s.q.GetBlockForNoteForUpdate(ctx, db.GetBlockForNoteForUpdateParams{
		ID:       pgUUID(blockID),
		NoteID:   pgUUID(noteID),
		Position: position,
	})
	return mapNoRows(err)
}

// GetBlockPropertiesForUpdate locks a block while reading only its properties.
func (s *Store) GetBlockPropertiesForUpdate(ctx context.Context, noteID, blockID uuid.UUID) (json.RawMessage, error) {
	properties, err := s.q.GetBlockPropertiesForUpdate(ctx, db.GetBlockPropertiesForUpdateParams{
		ID:     pgUUID(blockID),
		NoteID: pgUUID(noteID),
	})
	return NormalizeJSON(json.RawMessage(properties)), mapNoRows(err)
}

// GetBlockTextForUpdate locks a block while reading only its text.
func (s *Store) GetBlockTextForUpdate(ctx context.Context, noteID, blockID uuid.UUID) (string, error) {
	text, err := s.q.GetBlockTextForUpdate(ctx, db.GetBlockTextForUpdateParams{
		ID:     pgUUID(blockID),
		NoteID: pgUUID(noteID),
	})
	return text, mapNoRows(err)
}

// CreateNote inserts current note state. Sync also inserts the corresponding
// note_changes row in the same transaction.
func (s *Store) CreateNote(ctx context.Context, note Note) error {
	err := s.q.CreateNote(ctx, db.CreateNoteParams{
		ID:             pgUUID(note.ID),
		OwnerID:        pgUUID(note.OwnerID),
		Title:          note.Title,
		Metadata:       []byte(NormalizeJSON(note.Metadata)),
		CurrentVersion: note.CurrentVersion,
	})
	return err
}

// UpdateNoteMetadata writes only metadata and the resulting version.
func (s *Store) UpdateNoteMetadata(ctx context.Context, noteID, ownerID uuid.UUID, metadata json.RawMessage, version int64) error {
	return s.q.UpdateNoteMetadata(ctx, db.UpdateNoteMetadataParams{
		ID:             pgUUID(noteID),
		OwnerID:        pgUUID(ownerID),
		Metadata:       []byte(NormalizeJSON(metadata)),
		CurrentVersion: version,
	})
}

// UpdateNoteTitle writes only title and the resulting version.
func (s *Store) UpdateNoteTitle(ctx context.Context, noteID, ownerID uuid.UUID, title string, version int64) error {
	return s.q.UpdateNoteTitle(ctx, db.UpdateNoteTitleParams{
		ID:             pgUUID(noteID),
		OwnerID:        pgUUID(ownerID),
		Title:          title,
		CurrentVersion: version,
	})
}

// IncrementNoteVersion updates only the parent note version.
func (s *Store) IncrementNoteVersion(ctx context.Context, noteID, ownerID uuid.UUID, version int64) error {
	return s.q.IncrementNoteVersion(ctx, db.IncrementNoteVersionParams{
		ID:             pgUUID(noteID),
		OwnerID:        pgUUID(ownerID),
		CurrentVersion: version,
	})
}

// DeleteNote soft deletes a note and updates its version.
func (s *Store) DeleteNote(ctx context.Context, noteID, ownerID uuid.UUID, version int64) error {
	return s.q.DeleteNote(ctx, db.DeleteNoteParams{
		ID:             pgUUID(noteID),
		OwnerID:        pgUUID(ownerID),
		CurrentVersion: version,
	})
}

// CreateBlock inserts current block state.
func (s *Store) CreateBlock(ctx context.Context, block NoteBlock) error {
	err := s.q.CreateBlock(ctx, db.CreateBlockParams{
		ID:          pgUUID(block.ID),
		NoteID:      pgUUID(block.NoteID),
		BlockType:   block.BlockType,
		TextContent: block.TextContent,
		Position:    block.Position,
		Properties:  []byte(NormalizeJSON(block.Properties)),
	})
	return err
}

// UpdateBlockProperties writes only block properties.
func (s *Store) UpdateBlockProperties(ctx context.Context, noteID, blockID uuid.UUID, properties json.RawMessage) error {
	return s.q.UpdateBlockProperties(ctx, db.UpdateBlockPropertiesParams{
		ID:         pgUUID(blockID),
		NoteID:     pgUUID(noteID),
		Properties: []byte(NormalizeJSON(properties)),
	})
}

// UpdateBlockText writes only block text.
func (s *Store) UpdateBlockText(ctx context.Context, noteID, blockID uuid.UUID, text string) error {
	return s.q.UpdateBlockText(ctx, db.UpdateBlockTextParams{
		ID:          pgUUID(blockID),
		NoteID:      pgUUID(noteID),
		TextContent: text,
	})
}

// DeleteBlock soft deletes a block.
func (s *Store) DeleteBlock(ctx context.Context, noteID, blockID uuid.UUID) error {
	return s.q.DeleteBlock(ctx, db.DeleteBlockParams{
		ID:     pgUUID(blockID),
		NoteID: pgUUID(noteID),
	})
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

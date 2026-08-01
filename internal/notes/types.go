package notes

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NoteResponse is the current note state returned by direct recovery/debug read
// endpoints.
type NoteResponse struct {
	// ID is the note UUID.
	ID uuid.UUID `json:"id"`
	// OwnerID is included for diagnostics and should match the authenticated user.
	OwnerID uuid.UUID `json:"ownerId"`
	// CategoryID is optional because notes do not need a category.
	CategoryID *uuid.UUID `json:"categoryId,omitempty"`
	// Title is stored as a normal column for querying/display.
	Title string `json:"title"`
	// Metadata contains optional note-level JSONB properties.
	Metadata json.RawMessage `json:"metadata"`
	// CurrentVersion is the server-authoritative note version.
	CurrentVersion int64     `json:"currentVersion"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	// DeletedAt is present for tombstoned notes.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	// Blocks are ordered by position and include tombstones.
	Blocks []BlockResponse `json:"blocks"`
}

// BlockResponse is the API representation of a note block.
type BlockResponse struct {
	// ID is the block UUID.
	ID uuid.UUID `json:"id"`
	// NoteID is the owning note UUID.
	NoteID uuid.UUID `json:"noteId"`
	// BlockType is one of the supported block type strings.
	BlockType string `json:"blockType"`
	// TextContent is the user-visible text for text-like blocks.
	TextContent string `json:"textContent"`
	// Position is the string-based fractional ordering key.
	Position string `json:"position"`
	// Properties contains type-specific JSONB fields.
	Properties json.RawMessage `json:"properties"`
	// CurrentVersion is the server-authoritative block version.
	CurrentVersion int64     `json:"currentVersion"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	// DeletedAt is present for tombstoned blocks.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// SnapshotResponse exposes the latest stored full-note snapshot.
type SnapshotResponse struct {
	// ID is the snapshot UUID.
	ID uuid.UUID `json:"id"`
	// NoteID is the note represented by the snapshot.
	NoteID uuid.UUID `json:"noteId"`
	// OwnerID scopes the snapshot to the authenticated user.
	OwnerID uuid.UUID `json:"ownerId"`
	// Version is the note version represented by SnapshotData.
	Version int64 `json:"version"`
	// SnapshotFormat identifies the JSON document format.
	SnapshotFormat string `json:"snapshotFormat"`
	// SchemaVersion allows future snapshot schema evolution.
	SchemaVersion int32 `json:"schemaVersion"`
	// SnapshotData is the full note document JSON.
	SnapshotData json.RawMessage `json:"snapshotData"`
	// Checksum is a SHA-256 digest of SnapshotData.
	Checksum  string    `json:"checksum"`
	CreatedAt time.Time `json:"createdAt"`
}

package notes

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type NoteResponse struct {
	ID             uuid.UUID       `json:"id"`
	OwnerID        uuid.UUID       `json:"ownerId"`
	CategoryID     *uuid.UUID      `json:"categoryId,omitempty"`
	Title          string          `json:"title"`
	Metadata       json.RawMessage `json:"metadata"`
	CurrentVersion int64           `json:"currentVersion"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
	Blocks         []BlockResponse `json:"blocks"`
}

type BlockResponse struct {
	ID             uuid.UUID       `json:"id"`
	NoteID         uuid.UUID       `json:"noteId"`
	BlockType      string          `json:"blockType"`
	TextContent    string          `json:"textContent"`
	Position       string          `json:"position"`
	Properties     json.RawMessage `json:"properties"`
	CurrentVersion int64           `json:"currentVersion"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
}

type SnapshotResponse struct {
	ID             uuid.UUID       `json:"id"`
	NoteID         uuid.UUID       `json:"noteId"`
	OwnerID        uuid.UUID       `json:"ownerId"`
	Version        int64           `json:"version"`
	SnapshotFormat string          `json:"snapshotFormat"`
	SchemaVersion  int32           `json:"schemaVersion"`
	SnapshotData   json.RawMessage `json:"snapshotData"`
	Checksum       string          `json:"checksum"`
	CreatedAt      time.Time       `json:"createdAt"`
}

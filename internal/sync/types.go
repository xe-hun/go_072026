package syncapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const ProtocolVersion = 1

type Request struct {
	ProtocolVersion int               `json:"protocolVersion"`
	ClientVersion   string            `json:"clientVersion,omitempty"`
	DeviceID        uuid.UUID         `json:"deviceId"`
	Cursor          int64             `json:"cursor"`
	Limit           int32             `json:"limit,omitempty"`
	Operations      []ClientOperation `json:"operations"`
}

type ClientOperation struct {
	OperationID      uuid.UUID       `json:"operationId"`
	NoteID           uuid.UUID       `json:"noteId"`
	BlockID          *uuid.UUID      `json:"blockId,omitempty"`
	EntityType       string          `json:"entityType"`
	OperationType    string          `json:"operationType"`
	BaseNoteVersion  int64           `json:"baseNoteVersion"`
	BaseBlockVersion *int64          `json:"baseBlockVersion,omitempty"`
	ChangeFormat     string          `json:"changeFormat,omitempty"`
	ChangeData       json.RawMessage `json:"changeData"`
}

type Response struct {
	Accepted       []AcceptedOperation `json:"accepted"`
	Rejected       []RejectedOperation `json:"rejected"`
	Changes        []PulledChange      `json:"changes"`
	NextCursor     int64               `json:"nextCursor"`
	HasMore        bool                `json:"hasMore"`
	ResyncRequired bool                `json:"resyncRequired"`
	Reason         string              `json:"reason,omitempty"`
}

type AcceptedOperation struct {
	OperationID  uuid.UUID  `json:"operationId"`
	NoteID       uuid.UUID  `json:"noteId"`
	BlockID      *uuid.UUID `json:"blockId,omitempty"`
	NoteVersion  int64      `json:"noteVersion"`
	BlockVersion *int64     `json:"blockVersion,omitempty"`
	Sequence     int64      `json:"sequence"`
}

type RejectedOperation struct {
	OperationID        uuid.UUID  `json:"operationId"`
	Code               string     `json:"code"`
	Message            string     `json:"message,omitempty"`
	NoteID             uuid.UUID  `json:"noteId,omitempty"`
	BlockID            *uuid.UUID `json:"blockId,omitempty"`
	ClientNoteVersion  int64      `json:"clientNoteVersion,omitempty"`
	ServerNoteVersion  int64      `json:"serverNoteVersion,omitempty"`
	ClientBlockVersion *int64     `json:"clientBlockVersion,omitempty"`
	ServerBlockVersion *int64     `json:"serverBlockVersion,omitempty"`
}

type PulledChange struct {
	ID                    uuid.UUID       `json:"id"`
	OperationID           uuid.UUID       `json:"operationId"`
	NoteID                uuid.UUID       `json:"noteId"`
	BlockID               *uuid.UUID      `json:"blockId,omitempty"`
	DeviceID              uuid.UUID       `json:"deviceId"`
	EntityType            string          `json:"entityType"`
	OperationType         string          `json:"operationType"`
	BaseNoteVersion       int64           `json:"baseNoteVersion"`
	ResultingNoteVersion  int64           `json:"resultingNoteVersion"`
	BaseBlockVersion      *int64          `json:"baseBlockVersion,omitempty"`
	ResultingBlockVersion *int64          `json:"resultingBlockVersion,omitempty"`
	ChangeFormat          string          `json:"changeFormat"`
	SchemaVersion         int32           `json:"schemaVersion"`
	ChangeData            json.RawMessage `json:"changeData"`
	Sequence              int64           `json:"sequence"`
	CreatedAt             time.Time       `json:"createdAt"`
}

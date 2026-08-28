package syncapi

import (
	"fmt"
	"github.com/google/uuid"
	"net/http"
	constants "notes-server/internal"
	"notes-server/internal/httpapi"
)

// state.
func (r Request) Validate(maxOperations int) error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	if len(r.Operations) > maxOperations {
		return httpapi.NewError(http.StatusRequestEntityTooLarge, httpapi.CodePayloadTooLarge, "The sync request contains too many operations.")
	}
	return nil
}

// Validate checks pull request query parameters.
func (r PullRequest) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	if r.Cursor < 0 {
		return httpapi.InvalidRequest(constants.RequestKeyCursor + " must be greater than or equal to zero.")
	}
	return nil
}

// Validate checks update availability query parameters.
func (r UpdatesAvailableRequest) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return httpapi.NewError(http.StatusBadRequest, httpapi.CodeUnsupportedProtocol, "The requested sync protocol is not supported.")
	}
	if r.DeviceID == "" {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " is required.")
	}
	if !constants.ValidDeviceID(r.DeviceID) {
		return httpapi.InvalidRequest(constants.RequestKeyDeviceID + " must be 30 characters or fewer.")
	}
	return nil
}

// Validate checks operation-level invariants before database state is read.
func (op ClientOperation) Validate() error {
	if op.OperationID == uuid.Nil {
		return fmt.Errorf("%s is required", constants.RequestKeyOperationID)
	}
	if op.Sequence < 0 {
		return fmt.Errorf("%s must be greater than or equal to zero", constants.RequestKeySequence)
	}
	switch op.OperationType {
	case OperationCreateCategory, OperationDeleteCategory, OperationModifyCategory:
		if op.NoteID != uuid.Nil {
			return fmt.Errorf("%s is not allowed for category operations", constants.RequestKeyNoteID)
		}
	case OperationCreateNote, OperationDeleteNote, OperationModifyNoteProperty, OperationModifyNoteTitle:
		if op.NoteID == uuid.Nil {
			return fmt.Errorf("%s is required", constants.RequestKeyNoteID)
		}
	case OperationCreateBlock, OperationDeleteBlock, OperationModifyBlock:
		if op.NoteID == uuid.Nil {
			return fmt.Errorf("%s is required", constants.RequestKeyNoteID)
		}
	default:
		return fmt.Errorf("%s is unsupported", constants.RequestKeyOperationType)
	}
	if normalizeChangeFormat(op.ChangeFormat) != ChangeFormatStructuredV1 {
		return fmt.Errorf("%s is unsupported", constants.RequestKeyChangeFormat)
	}
	return nil
}

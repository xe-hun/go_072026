package syncapi

import "notes-server/internal/httpapi"

// rejectedInvalid builds a per-operation rejection for malformed operation data.
func rejectedInvalid(op ClientOperation, message string) RejectedOperation {
	return RejectedOperation{
		OperationID: op.OperationID,
		Code:        httpapi.CodeInvalidRequest,
		Message:     message,
		NoteID:      op.NoteID,
		BlockID:     op.BlockID,
	}
}

// rejectedNotFound reports an operation that references a missing note/block.
func rejectedNotFound(op ClientOperation, message string) RejectedOperation {
	return RejectedOperation{
		OperationID: op.OperationID,
		Code:        httpapi.CodeNotFound,
		Message:     message,
		NoteID:      op.NoteID,
		BlockID:     op.BlockID,
	}
}

// rejectedConflict reports base-version mismatches so clients can resolve the
// conflict explicitly.
func rejectedConflict(op ClientOperation, serverNoteVersion int64, serverBlockVersion *int64) RejectedOperation {
	return RejectedOperation{
		OperationID:        op.OperationID,
		Code:               httpapi.CodeBaseVersionConflict,
		NoteID:             op.NoteID,
		BlockID:            op.BlockID,
		ClientNoteVersion:  op.BaseNoteVersion,
		ServerNoteVersion:  serverNoteVersion,
		ServerBlockVersion: serverBlockVersion,
	}
}

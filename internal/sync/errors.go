package syncapi

import "notes-server/internal/httpapi"

// rejectedInvalid builds a per-operation rejection for malformed operation data.
func rejectedInvalid(op ClientOperation, message string) RejectedDTO {
	return RejectedDTO{
		Code:    httpapi.CodeInvalidRequest,
		Reason:  RejectionReasonInvalidRequest,
		Message: message,
		NoteID:  op.NoteID,
	}
}

// rejectedNotFound reports an operation that references a missing note/block.
func rejectedNotFound(op ClientOperation, message string) RejectedDTO {
	return RejectedDTO{
		Code:    httpapi.CodeNotFound,
		Reason:  RejectionReasonNotFound,
		Message: message,
		NoteID:  op.NoteID,
	}
}

// rejectedConflict reports note base-version mismatches so clients can resolve
// the conflict explicitly.
func rejectedConflict(op ClientOperation, serverNoteVersion int64) RejectedDTO {
	return RejectedDTO{
		Code:              httpapi.CodeBaseVersionConflict,
		Reason:            RejectionReasonVersionConflict,
		Message:           "The note has changed since it was last synchronized.",
		NoteID:            op.NoteID,
		ClientNoteVersion: op.BaseNoteVersion,
		ServerNoteVersion: serverNoteVersion,
	}
}

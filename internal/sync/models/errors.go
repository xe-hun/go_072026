package models

import "notes-server/internal/httpapi"

// rejectedInvalid builds a per-operation rejection for malformed operation data.
func RejectedInvalid(op ClientOperation, message string) RejectedDTO {
	return RejectedDTO{
		Code:    httpapi.CodeInvalidRequest,
		Reason:  RejectionReasonInvalidRequest,
		Message: message,
		NoteID:  op.NoteID,
	}
}

// rejectedNotFound reports an operation that references a missing note/block.
func RejectedNotFound(op ClientOperation, message string) RejectedDTO {
	return RejectedDTO{
		Code:    httpapi.CodeNotFound,
		Reason:  RejectionReasonNotFound,
		Message: message,
		NoteID:  op.NoteID,
	}
}

// rejectedConflict reports note base-version mismatches so clients can resolve
// the conflict explicitly.
func RejectedConflict(op ClientOperation, serverNoteVersion int64) RejectedDTO {
	return RejectedDTO{
		Code:              httpapi.CodeBaseVersionConflict,
		Reason:            RejectionReasonVersionConflict,
		Message:           "The note has changed since it was last synchronized.",
		NoteID:            op.NoteID,
		ClientNoteVersion: op.BaseNoteVersion,
		ServerNoteVersion: serverNoteVersion,
	}
}

package syncapi

import "notes-server/internal/httpapi"

func rejectedInvalid(op ClientOperation, message string) RejectedOperation {
	return RejectedOperation{
		OperationID: op.OperationID,
		Code:        httpapi.CodeInvalidRequest,
		Message:     message,
		NoteID:      op.NoteID,
		BlockID:     op.BlockID,
	}
}

func rejectedNotFound(op ClientOperation, message string) RejectedOperation {
	return RejectedOperation{
		OperationID: op.OperationID,
		Code:        httpapi.CodeNotFound,
		Message:     message,
		NoteID:      op.NoteID,
		BlockID:     op.BlockID,
	}
}

func rejectedConflict(op ClientOperation, serverNoteVersion int64, serverBlockVersion *int64) RejectedOperation {
	return RejectedOperation{
		OperationID:        op.OperationID,
		Code:               httpapi.CodeBaseVersionConflict,
		NoteID:             op.NoteID,
		BlockID:            op.BlockID,
		ClientNoteVersion:  op.BaseNoteVersion,
		ServerNoteVersion:  serverNoteVersion,
		ClientBlockVersion: op.BaseBlockVersion,
		ServerBlockVersion: serverBlockVersion,
	}
}

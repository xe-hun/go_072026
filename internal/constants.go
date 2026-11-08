// Package constants defines shared protocol identifiers used by the server.
package constants

const (
	RequestKeyConflict = "note_conflict"

	//commonkeys
	// RequestKeyID         = "id"
	RequestKeyBlockId    = "blockId"
	RequestKeyCategoryId = "categoryId"

	// Request field names. Keep these values together so request parsing,
	// validation, and debugging use one canonical spelling.
	RequestKeyProtocolVersion = "protocolVersion"
	RequestKeyClientVersion   = "clientVersion"
	RequestKeyDeviceID        = "deviceId"
	RequestKeyOperations      = "operations"
	RequestKeyOperationID     = "operationId"
	RequestKeyOperationType   = "operationType"
	RequestKeyChangeFormat    = "changeFormat"
	RequestKeyCursor          = "cursor"
	RequestKeyDeviceName      = "deviceName"
	RequestKeyPlatform        = "platform"
	RequestKeyAppVersion      = "appVersion"

	// Text delta fields.
	RequestKeyTextDelta   = "textDelta"
	RequestKeyIndex       = "index"
	RequestKeyDeleteCount = "deleteCount"
	RequestKeyInsertText  = "insertText"

	// Note properties.
	RequestKeyNoteID          = "noteId"
	RequestKeyUploadID        = "uploadId"
	RequestKeySequence        = "sequence"
	RequestKeyTitle           = "title"
	RequestKeyBaseNoteVersion = "baseNoteVersion"
	RequestKeyChangeData      = "changeData"
	RequestKeyNoteProperties  = "noteProperties"
	RequestKeyName            = "categoryName"
	RequestKeyTextContent     = "textContent"
	RequestKeyBlocks          = "blocks"

	// Block properties.
	RequestKeyBlock     = "block"
	RequestKeyBlockType = "blockType"
	// RequestKeyPosition        = "position"
	RequestKeyPosition        = "fractionalPosition"
	RequestKeyBlockProperties = "blockProperties"

	// Supported block types.
	BlockTypeText         = "text"
	BlockTypeBullet       = "bullet"
	BlockTypeTodo         = "todo"
	BlockTypeNumberedList = "numberedList"
	BlockTypeAttachment   = "attachment"

	// Note operation names.
	OperationCreateNote         = "createNote"
	OperationDeleteNote         = "deleteNote"
	OperationModifyNoteProperty = "modifyNoteProperty"
	OperationModifyNoteTitle    = "modifyNoteTitle"

	// Block operation names.
	OperationCreateBlock = "createBlock"
	OperationDeleteBlock = "deleteBlock"
	OperationModifyBlock = "modifyBlock"

	// Category operation names.
	OperationCreateCategory = "createCategory"
	OperationDeleteCategory = "deleteCategory"
	OperationModifyCategory = "modifyCategory"

	// ChangeFormatStructuredV1 stores changed fields rather than raw character
	// diffs.
	ChangeFormatStructuredV1 = "structured-operation-v1"
)

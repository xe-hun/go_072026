// Package constants defines shared protocol identifiers used by the server.
package constants

import "unicode/utf8"

const (
	RequestKeyConflict = "note_conflict"

	//commonkeys
	// RequestKeyID         = "id"
	RequestKeyBlockId    = "blockId"
	RequestKeyCategoryId = "categoryId"

	// Request field names. Keep these values together so request parsing,
	// validation, and debugging use one canonical spelling.
	RequestKeyProtocolVersion  = "protocolVersion"
	RequestKeyClientVersion    = "clientVersion"
	RequestKeyDeviceID         = "deviceId"
	RequestKeyDeviceIdentifier = "device_identifier"
	RequestKeyOperations       = "operations"
	RequestKeyOperationID      = "operationId"
	RequestKeyOperationType    = "operationType"
	RequestKeyChangeFormat     = "changeFormat"
	RequestKeyCursor           = "cursor"
	RequestKeyDeviceName       = "deviceName"
	RequestKeyPlatform         = "platform"
	RequestKeyAppVersion       = "appVersion"

	DeviceIdentifierMaxLength = 100

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

// ValidDeviceIdentifier reports whether a registration device identifier is
// present and within the database column's maximum length.
func ValidDeviceIdentifier(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= DeviceIdentifierMaxLength
}

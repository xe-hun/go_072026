// Package constants defines shared protocol identifiers used by the server.
package constants

import "unicode/utf8"

const (
	// DeviceIDMaxLength is the maximum number of Unicode characters allowed in
	// a client-supplied device identifier.
	DeviceIDMaxLength = 30

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

	//note Properties
	RequestKeyNoteID            = "noteId"
	RequestKeySequence          = "sequence"
	RequestKeyTitle             = "title"
	RequestKeyBaseNoteVersion   = "baseNoteVersion"
	RequestKeyChangeData        = "changeData"
	RequestKeyNoteProperties    = "noteProperties"
	RequestKeyChangedProperties = "changedProperties"
	RequestKeyName              = "CategoryName"
	RequestKeyTextContent       = "textContent"
	RequestKeyBlocks            = "blocks"

	//block Properties
	RequestKeyBlock           = "block"
	RequestKeyBlockType       = "blockType"
	RequestKeyPosition        = "position"
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

// ValidDeviceID reports whether a device identifier is non-empty and within
// the protocol's maximum length.
func ValidDeviceID(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= DeviceIDMaxLength
}

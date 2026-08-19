// Package constants defines shared protocol identifiers used by the server.
package constants

const (
	// Note operation names.
	OperationCreateNote         = "create_note"
	OperationDeleteNote         = "delete_note"
	OperationModifyNoteProperty = "modify_note_property"
	OperationModifyNoteTitle    = "modify_note_title"

	// Block operation names.
	OperationCreateBlock = "create_block"
	OperationDeleteBlock = "delete_block"
	OperationModifyBlock = "modify_block"

	// Category operation names.
	OperationCreateCategory = "create_category"
	OperationDeleteCategory = "delete_category"
	OperationModifyCategory = "modify_category"

	// Text operation names accepted by title and block text deltas.
	TextOperationInsert = "insert"
	TextOperationDelete = "delete"

	// Supported block types.
	BlockTypeText         = "text"
	BlockTypeBullet       = "bullet"
	BlockTypeTodo         = "todo"
	BlockTypeNumberedList = "numbered_list"
	BlockTypeAttachment   = "attachment"

	// ChangeFormatStructuredV1 stores changed fields rather than raw character
	// diffs.
	ChangeFormatStructuredV1 = "structured-operation-v1"
)

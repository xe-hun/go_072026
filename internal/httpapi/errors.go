package httpapi

import (
	"errors"
	"net/http"
)

const (
	CodeInvalidRequest      = "INVALID_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeDeviceRevoked       = "DEVICE_REVOKED"
	CodeUnsupportedProtocol = "UNSUPPORTED_PROTOCOL"
	CodeBaseVersionConflict = "BASE_VERSION_CONFLICT"
	CodeCursorExpired       = "CURSOR_EXPIRED"
	CodePayloadTooLarge     = "PAYLOAD_TOO_LARGE"
	CodeRateLimited         = "RATE_LIMITED"
	CodeInternalError       = "INTERNAL_ERROR"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func InvalidRequest(message string) *Error {
	return NewError(http.StatusBadRequest, CodeInvalidRequest, message)
}

func Unauthorized() *Error {
	return NewError(http.StatusUnauthorized, CodeUnauthorized, "Authentication is required.")
}

func Forbidden(message string) *Error {
	return NewError(http.StatusForbidden, CodeForbidden, message)
}

func NotFound(message string) *Error {
	return NewError(http.StatusNotFound, CodeNotFound, message)
}

func Internal() *Error {
	return NewError(http.StatusInternalServerError, CodeInternalError, "An internal error occurred.")
}

func FromError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal()
}

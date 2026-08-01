package httpapi

import (
	"errors"
	"net/http"
)

const (
	// These code strings are the public API contract for error responses. Keep
	// them stable even if internal Go error types change.
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

// Error is the internal representation of an API-safe error. Handlers can return
// this directly without leaking raw SQL, JWT, or stack-trace details.
type Error struct {
	// Status is the HTTP status code to write.
	Status int
	// Code is the stable machine-readable error code.
	Code string
	// Message is safe for clients to display.
	Message string
	// Details optionally carries small structured validation details.
	Details map[string]any
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// NewError constructs an API error with an explicit HTTP status and public code.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// InvalidRequest returns a 400 error for malformed or unsupported client input.
func InvalidRequest(message string) *Error {
	return NewError(http.StatusBadRequest, CodeInvalidRequest, message)
}

// Unauthorized returns a 401 error without describing the token validation
// failure.
func Unauthorized() *Error {
	return NewError(http.StatusUnauthorized, CodeUnauthorized, "Authentication is required.")
}

// Forbidden returns a 403 error for authenticated users who cannot perform an
// action.
func Forbidden(message string) *Error {
	return NewError(http.StatusForbidden, CodeForbidden, message)
}

// NotFound returns a 404 error for resources outside the authenticated user's
// scope or resources that do not exist.
func NotFound(message string) *Error {
	return NewError(http.StatusNotFound, CodeNotFound, message)
}

// Internal returns a generic 500 error that hides implementation details.
func Internal() *Error {
	return NewError(http.StatusInternalServerError, CodeInternalError, "An internal error occurred.")
}

// FromError preserves explicit API errors and maps all unknown errors to a safe
// generic internal error.
func FromError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal()
}

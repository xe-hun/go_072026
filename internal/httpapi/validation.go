package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// DecodeJSON decodes one request body into dst, rejects unknown fields, maps body
// size errors to PAYLOAD_TOO_LARGE, and rejects trailing JSON documents.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// Unknown JSON properties often indicate a client/server contract mismatch,
	// so fail early instead of silently dropping data.
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return NewError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "The request body is too large.")
		}
		return InvalidRequest("The request body must be valid JSON.")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return NewError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "The request body is too large.")
		}
		return InvalidRequest("The request body must contain a single JSON document.")
	}
	return nil
}

// MapDecodeError normalizes decode errors into API errors. It is kept separate
// so handlers or tests can map decode failures without writing a response.
func MapDecodeError(err error) *Error {
	if err == nil {
		return nil
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return NewError(http.StatusBadRequest, CodeInvalidRequest, "The request is invalid.")
}

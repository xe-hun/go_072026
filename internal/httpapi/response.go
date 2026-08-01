package httpapi

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the stable top-level JSON shape for API errors.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// errorBody mirrors the public error contract documented in the specification.
type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

// WriteJSON writes a JSON response. Encoding errors are ignored because the
// values passed by handlers are controlled application structs.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// WriteNoContent writes an empty 204 response for successful commands that do
// not return a resource.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// WriteError converts any Go error into the API error envelope and includes the
// request ID set by middleware.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := FromError(err)
	WriteJSON(w, apiErr.Status, errorEnvelope{
		Error: errorBody{
			Code:      apiErr.Code,
			Message:   apiErr.Message,
			RequestID: RequestIDFromContext(r.Context()),
			Details:   apiErr.Details,
		},
	})
}

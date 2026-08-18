package httpapi

import (
	"encoding/json"
	"log/slog"
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

// WriteJSON writes a JSON response and logs transport/encoding failures. At
// this point headers may already have been sent, so logging is the only safe
// way to surface a failed response write.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json response", "status", status, "error", err)
	}
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
	slog.ErrorContext(r.Context(), "http request error",
		"request_id", RequestIDFromContext(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"status", apiErr.Status,
		"code", apiErr.Code,
		"error", err,
	)
	WriteJSON(w, apiErr.Status, errorEnvelope{
		Error: errorBody{
			Code:      apiErr.Code,
			Message:   apiErr.Message,
			RequestID: RequestIDFromContext(r.Context()),
			Details:   apiErr.Details,
		},
	})
}

package httpapi

import (
	"encoding/json"
	"net/http"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

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

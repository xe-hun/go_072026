package middleware

import (
	"net/http"

	"github.com/google/uuid"

	"notes-server/internal/httpapi"
)

// RequestID accepts an incoming X-Request-Id or generates a UUID, then stores it
// in context and echoes it in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(w, r.WithContext(httpapi.WithRequestID(r.Context(), requestID)))
	})
}

package middleware

import (
	"log/slog"
	"net/http"

	"notes-server/internal/httpapi"
)

// Recovery catches panics, logs them with the request ID, and returns a generic
// internal error so implementation details are not exposed to clients.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						"request_id", httpapi.RequestIDFromContext(r.Context()),
						"panic", recovered,
					)
					httpapi.WriteError(w, r, httpapi.Internal())
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

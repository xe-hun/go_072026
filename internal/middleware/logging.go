package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

// statusRecorder wraps ResponseWriter so logging middleware can observe the
// final HTTP status and response size after the handler finishes.
type statusRecorder struct {
	http.ResponseWriter
	// status remains zero until WriteHeader or Write is called.
	status int
	// bytes counts response body bytes written by the handler.
	bytes int
}

// WriteHeader records the first status code. Later calls are ignored, matching
// net/http behavior.
func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write records implicit 200 responses and counts body bytes.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// RequestLogging writes one structured log event per request after the handler
// has returned.
func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}

			attrs := []any{
				"request_id", httpapi.RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"route", chi.RouteContext(r.Context()).RoutePattern(),
				"status", status,
				"bytes", recorder.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			// Add user_id only when auth middleware has already populated context.
			// Public routes such as /health naturally omit it.
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
				attrs = append(attrs, "user_id", claims.UserID.String())
			}
			logger.InfoContext(r.Context(), "http request", attrs...)
		})
	}
}

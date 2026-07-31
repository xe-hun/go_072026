package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"notes-server/internal/auth"
	"notes-server/internal/httpapi"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

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
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
				attrs = append(attrs, "user_id", claims.UserID.String())
			}
			logger.InfoContext(r.Context(), "http request", attrs...)
		})
	}
}

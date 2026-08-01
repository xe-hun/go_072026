package httpapi

import "context"

// requestIDKey is private to avoid collisions with context values from other
// packages.
type requestIDKey struct{}

// WithRequestID stores the request ID generated or accepted by middleware.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestIDFromContext returns the request ID for logs and error responses. It
// returns an empty string if middleware has not run.
func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

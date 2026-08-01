package middleware

import (
	"net/http"

	"notes-server/internal/httpapi"
)

// RateLimiter is a small extension point for a future production limiter. The
// implementation can inspect request IP, route, user, or headers.
type RateLimiter interface {
	Allow(r *http.Request) bool
}

// AllowAllLimiter is the development/default limiter. It preserves the
// middleware shape without introducing Redis or another required dependency.
type AllowAllLimiter struct{}

// Allow always permits the request.
func (AllowAllLimiter) Allow(*http.Request) bool {
	return true
}

// RateLimit calls the configured limiter before the handler. It returns the
// standard API error envelope when the limiter rejects a request.
func RateLimit(limiter RateLimiter) func(http.Handler) http.Handler {
	if limiter == nil {
		limiter = AllowAllLimiter{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(r) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusTooManyRequests, httpapi.CodeRateLimited, "Rate limit exceeded."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

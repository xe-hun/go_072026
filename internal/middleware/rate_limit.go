package middleware

import (
	"net/http"

	"notes-server/internal/httpapi"
)

type RateLimiter interface {
	Allow(r *http.Request) bool
}

type AllowAllLimiter struct{}

func (AllowAllLimiter) Allow(*http.Request) bool {
	return true
}

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

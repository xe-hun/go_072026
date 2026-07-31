package middleware

import (
	"net/http"
	"time"
)

func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, duration, `{"error":{"code":"INTERNAL_ERROR","message":"The request timed out."}}`)
	}
}

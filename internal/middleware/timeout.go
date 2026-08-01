package middleware

import (
	"net/http"
	"time"
)

// Timeout bounds request execution time. It is layered with server-level
// read/write timeouts for defense in depth.
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, duration, `{"error":{"code":"INTERNAL_ERROR","message":"The request timed out."}}`)
	}
}

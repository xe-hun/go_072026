package middleware

import "net/http"

// BodyLimit limits the compressed/wire-size body before optional gzip
// decompression. DecodeJSON maps MaxBytesReader errors to PAYLOAD_TOO_LARGE.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"notes-server/internal/httpapi"
)

// gzipReadCloser lets middleware replace r.Body with a decompressed reader while
// still closing both the decompressor and size-limited wrapper.
type gzipReadCloser struct {
	io.Reader
	// closers are closed in order when the request body is closed.
	closers []io.Closer
}

// Close closes every wrapped stream and returns the first close error.
func (g gzipReadCloser) Close() error {
	var first error
	for _, closer := range g.closers {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// GzipDecompress transparently handles Content-Encoding: gzip request bodies and
// enforces a decompressed-size limit to defend against compression bombs.
func GzipDecompress(maxDecompressedBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				httpapi.WriteError(w, r, httpapi.InvalidRequest("The request body is not valid gzip data."))
				return
			}
			limited := http.MaxBytesReader(w, reader, maxDecompressedBytes)
			// Downstream JSON decoding sees the decompressed stream exactly as if
			// the client had sent plain JSON.
			r.Body = gzipReadCloser{Reader: limited, closers: []io.Closer{limited, reader}}
			r.Header.Del("Content-Encoding")
			next.ServeHTTP(w, r)
		})
	}
}

// gzipResponseWriter redirects handler writes through a gzip.Writer while
// preserving the original ResponseWriter for headers and status codes.
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

// Write compresses response body bytes.
func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.writer.Write(b)
}

// Flush pushes pending compressed bytes and forwards flushes to the underlying
// writer when supported.
func (w gzipResponseWriter) Flush() {
	_ = w.writer.Flush()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// GzipResponse compresses responses for clients that advertise gzip support.
func GzipResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzip.NewWriter(w)
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: gz}, r)
	})
}

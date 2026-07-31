package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"notes-server/internal/httpapi"
)

type gzipReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (g gzipReadCloser) Close() error {
	var first error
	for _, closer := range g.closers {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

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
			r.Body = gzipReadCloser{Reader: limited, closers: []io.Closer{limited, reader}}
			r.Header.Del("Content-Encoding")
			next.ServeHTTP(w, r)
		})
	}
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.writer.Write(b)
}

func (w gzipResponseWriter) Flush() {
	_ = w.writer.Flush()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

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

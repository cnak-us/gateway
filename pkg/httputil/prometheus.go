package httputil

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// RecordHTTPRequestFunc is the signature for the metric recording callback.
type RecordHTTPRequestFunc func(method, path, status string, durationSeconds float64)

// PrometheusMiddleware returns middleware that records HTTP request metrics.
// It wraps the response writer to capture the status code and uses
// chi.RouteContext for the path label to avoid high cardinality from dynamic
// URL segments.
func PrometheusMiddleware(record RecordHTTPRequestFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			// Use the chi route pattern (e.g. "/api/points/{id}") instead of
			// the raw URL path to keep the cardinality of the "path" label bounded.
			path := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					path = pattern
				}
			}

			record(
				r.Method,
				path,
				fmt.Sprintf("%d", sw.status),
				time.Since(start).Seconds(),
			)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap allows the chi middleware stack (and http.ResponseController) to
// access the original ResponseWriter and its optional interfaces such as
// http.Flusher, which is required for SSE streaming endpoints.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

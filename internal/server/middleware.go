package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/logging"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// RequestIDFromContext returns the request_id if present.
func RequestIDFromContext(ctx context.Context) string {
	v := ctx.Value(requestIDKey)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// LoggingMiddleware logs one line per request with request_id + latency + status.
func LoggingMiddleware(lg *logging.Log, next http.Handler) http.Handler {
	if lg == nil {
		// no-op if logger not provided
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Use incoming request id if provided, else generate one
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = newRequestID()
		}

		// Put request_id in context and response header
		ctx := context.WithValue(r.Context(), requestIDKey, rid)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-Id", rid)

		// capture status code
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		// log at end
		lg.Info("http_request", map[string]any{
			"request_id":  rid,
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      sw.status,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
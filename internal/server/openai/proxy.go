package openai

import (
	"net/http"
	"time"
)

type Logger interface {
	Info(msg string, fields map[string]any)
	Error(msg string, fields map[string]any)
}

func LoggingMiddleware(l Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		l.Info("http_request", map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"took_ms": time.Since(start).Milliseconds(),
		})
	})
}

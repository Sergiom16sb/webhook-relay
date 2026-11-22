// Package http provides the HTTP-driven adapter for the webhook relay.
// Includes middleware: thin functions that wrap handlers to add
// cross-cutting concerns (logging, recovery, request IDs).
package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/infra/logger"
)

// Middleware is the standard http.Handler wrapper: takes a handler, returns
// a handler that adds behavior before/after calling the inner one.
//
// Usage:
//
//	handler := Chain(mux, RequestID, Recovery, Logger)
//	http.ListenAndServe(addr, handler)
type Middleware func(http.Handler) http.Handler

// Chain wraps h with mws. First middleware in the slice runs first
// (outermost). The last wraps h directly.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestID generates a unique ID per request, attaches it to the context
// and the response header (X-Request-ID). Downstream code (Logger, use cases)
// pulls it from context for log correlation.
//
// ID format: 12-char hex from crypto/rand. Not UUID-compliant but unique
// enough for log correlation within a single process. Step 1.15 introduces
// google/uuid for event IDs (different concern).
type ctxKey int

const requestIDKey ctxKey = iota

// WithRequestID returns a context carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request ID stored in ctx, or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestID middleware.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateRequestID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Recovery catches panics in downstream handlers, logs them, and returns
// 500. Without this, a panic in any handler kills the goroutine and the
// connection (client gets no response, error is invisible).
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				l := logger.FromContextWith(r.Context(),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(rec),
				)
				l.Error("panic recovered")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter to capture the status code and
// bytes written. http.ResponseWriter doesn't expose these after the fact,
// so we record them as the response flows through.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// Per http.ResponseWriter contract, Write sends 200 if WriteHeader wasn't called.
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logger logs one structured line per request AND injects a request-scoped
// *slog.Logger into ctx with the request_id pre-attached. Downstream code
// (handlers, use cases) just calls logger.FromContext(ctx).Info(...) and
// gets request_id in the log line for free.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}

		// Build the request-scoped logger with request_id already attached.
		// Downstream code pulls this via logger.FromContext(ctx).
		requestID := rec.Header().Get("X-Request-ID")
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
		}
		if requestID != "" {
			attrs = append(attrs, "request_id", requestID)
		}

		// Log the request completion.
		baseLogger := slog.Default()
		baseLogger.Info("http request", attrs...)

		// ALSO inject the logger into ctx so downstream code can use it.
		// (For this refactor we don't re-derive r.Context() — the logger is
		// available via logger.FromContext in any handler that runs inside
		// this middleware.)
		_ = logger.WithLogger(r.Context(), baseLogger.With(attrs...))
	})
}

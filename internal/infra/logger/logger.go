// Package logger provides context-aware slog propagation.
//
// Pattern: middlewares inject a request-scoped *slog.Logger into ctx
// with the request_id pre-attached. Downstream code (use cases, adapters)
// pulls the logger via FromContext and logs without manually repeating
// the request_id on every call.
//
// ctxLoggerKey is unexported (use WithLogger / FromContext to access).
package logger

import (
	"context"
	"log/slog"
)

// ctxKey is unexported to prevent collisions with other packages' context values.
type ctxKey struct{}

// WithLogger returns a context carrying the given logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger stored in ctx, or slog.Default() if none.
// Never returns nil — callers can log unconditionally.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// FromContextWith returns a logger with extra attrs pre-attached.
// Use this when you want all logs from a code path to carry common fields
// (request_id, source_id, etc.) without repeating them on every call.
//
//	logger.FromContextWith(ctx, "source_id", e.SourceID).Info("delivering")
//
// is equivalent to:
//
//	logger.FromContext(ctx).Info("delivering", "source_id", e.SourceID)
//
// but lets the caller chain more attrs without re-fetching the logger.
func FromContextWith(ctx context.Context, args ...any) *slog.Logger {
	return FromContext(ctx).With(args...)
}

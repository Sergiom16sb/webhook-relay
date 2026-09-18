package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/sgtour/webhook-relay/internal/infra/logger"
)

// captureLogger returns a logger that writes JSON to buf, plus a helper
// to read the last line. Lets tests assert on log fields.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func TestFromContext_NoLoggerInContext_ReturnsDefault(t *testing.T) {
	l := logger.FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext must never return nil")
	}
	// Default is the package-level slog.Default(). Verify by logging.
	// (No assertion on output — just that the call doesn't panic.)
	l.Info("test")
}

func TestWithLogger_Roundtrip(t *testing.T) {
	var buf bytes.Buffer
	custom := captureLogger(&buf)
	ctx := logger.WithLogger(context.Background(), custom)

	got := logger.FromContext(ctx)
	if got != custom {
		t.Errorf("FromContext returned wrong logger")
	}

	// Logging via the context-derived logger should write to buf.
	logger.FromContext(ctx).Info("hello", "k", "v")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON log: %v\n%s", err, buf.String())
	}
	if line["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", line["msg"])
	}
	if line["k"] != "v" {
		t.Errorf("k = %v, want v", line["k"])
	}
}

func TestFromContextWith_AttachesFields(t *testing.T) {
	var buf bytes.Buffer
	custom := captureLogger(&buf)
	ctx := logger.WithLogger(context.Background(), custom)

	logger.FromContextWith(ctx, "request_id", "abc-123").Info("processed")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if line["request_id"] != "abc-123" {
		t.Errorf("request_id = %v, want abc-123", line["request_id"])
	}
}

func TestFromContextWith_NoFields_ReturnsBase(t *testing.T) {
	var buf bytes.Buffer
	custom := captureLogger(&buf)
	ctx := logger.WithLogger(context.Background(), custom)

	// With no extra args, should behave like FromContext (no extra attrs).
	l := logger.FromContextWith(ctx)
	if l == nil {
		t.Fatal("FromContextWith with no args returned nil")
	}
	l.Info("ok")

	var line map[string]any
	_ = json.Unmarshal(buf.Bytes(), &line)
	if _, has := line["request_id"]; has {
		t.Errorf("empty FromContextWith should NOT add attrs, got %v", line)
	}
}

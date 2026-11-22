package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infrahttp "github.com/Sergiom16sb/webhook-relay/internal/infra/http"
)

func TestRequestID_GeneratesUniqueIDs(t *testing.T) {
	var seen []string
	handler := infrahttp.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := infrahttp.RequestIDFromContext(r.Context())
		seen = append(seen, id)
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Make 5 requests, each should get a unique ID.
	for i := 0; i < 5; i++ {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		id := resp.Header.Get("X-Request-ID")
		if id == "" {
			t.Fatalf("missing X-Request-ID header on request %d", i)
		}
		for _, prev := range seen[:len(seen)-1] {
			if prev == id {
				t.Errorf("duplicate request ID: %s", id)
			}
		}
	}
}

func TestRequestID_AvailableInHandler(t *testing.T) {
	var gotID string
	handler := infrahttp.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = infrahttp.RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("handler did not see request ID")
	}
	if gotID != rec.Header().Get("X-Request-ID") {
		t.Errorf("context ID %q != header ID %q", gotID, rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestID_AvailableWithoutMiddleware(t *testing.T) {
	// Direct WithRequestID usage — no middleware chain.
	ctx := infrahttp.WithRequestID(context.Background(), "test-id")
	if got := infrahttp.RequestIDFromContext(ctx); got != "test-id" {
		t.Errorf("got %q, want test-id", got)
	}

	// Empty context returns "".
	if got := infrahttp.RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestRecovery_PanicReturns500(t *testing.T) {
	handler := infrahttp.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Recovery must NOT propagate the panic — that's the whole point.
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Recovery did not catch panic: %v", rec)
		}
	}()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "internal server error") {
		t.Errorf("body = %q, want it to contain 'internal server error'", body)
	}
}

func TestRecovery_NoPanicPassesThrough(t *testing.T) {
	handler := infrahttp.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418 (handler's response, not Recovery's)", rec.Code)
	}
}

func TestLogger_LogsRequestLine(t *testing.T) {
	// Capture slog output by replacing the default logger with a buffer-backed one.
	var buf bytes.Buffer
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(origLogger)

	handler := infrahttp.Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("invalid JSON log: %v\nraw: %s", err, buf.String())
	}
	if got["method"] != "GET" {
		t.Errorf("method = %v, want GET", got["method"])
	}
	if got["path"] != "/test" {
		t.Errorf("path = %v, want /test", got["path"])
	}
	if got["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got["status"])
	}
	if _, ok := got["duration_ms"]; !ok {
		t.Errorf("missing duration_ms field")
	}
	if got["bytes"] != float64(2) {
		t.Errorf("bytes = %v, want 2 (wrote 'ok')", got["bytes"])
	}
}

func TestLogger_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(origLogger)

	// Wrap with both RequestID and Logger — RequestID first so its
	// context value is available when Logger reads it.
	handler := infrahttp.Logger(infrahttp.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var got map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got)
	if got["request_id"] == nil || got["request_id"] == "" {
		t.Errorf("log line missing request_id: %s", buf.String())
	}
}

func TestChain_OrderMatters(t *testing.T) {
	// Logger(Recovery(RequestID(handler))) means RequestID runs first.
	// Verify the chain produces the expected call order via a counter in shared state.
	var order []string
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})
	pre := func(label string) infrahttp.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, label+":before")
				next.ServeHTTP(w, r)
				order = append(order, label+":after")
			})
		}
	}

	handler := infrahttp.Chain(final,
		pre("A"),
		pre("B"),
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// Expected order: A:before, B:before, handler, B:after, A:after
	// (A wraps B which wraps the handler.)
	want := []string{"A:before", "B:before", "handler", "B:after", "A:after"}
	if len(order) != len(want) {
		t.Fatalf("order length = %d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

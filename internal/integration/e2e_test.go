//go:build integration

// Package integration runs end-to-end tests with real Postgres via
// testcontainers + httptest. Run with:
//
//	go test -tags=integration ./internal/integration/...
//
// These tests are excluded from the default `go test ./...` (the //go:build
// integration tag prevents compilation without it).
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	pgadapter "github.com/sgtour/webhook-relay/internal/infra/postgres"
	"github.com/sgtour/webhook-relay/internal/infra/webhook"
	"github.com/sgtour/webhook-relay/internal/infra/worker"
)

// --- Test helpers ---

// signPayload computes HMAC-SHA256 of payload with secret.
// Uses the webhook package's algorithm (same code path as production).
func signPayload(secret string, payload []byte) string {
	v, _ := webhook.NewHMACVerifier(secret)
	return v.Sign(payload)
}

// hashPayload returns sha256 hex of payload. Matches ReceiveEventUseCase's
// internal hashPayload.
func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// fmtPayload builds a JSON payload string with the given id and a fixed
// inner object. Convenience for tests.
func fmtPayload(id string) string {
	return fmt.Sprintf(`{"id":%q,"source_id":"src","type":"t","payload":{"k":"v"}}`, id)
}

// setup brings up Postgres + returns a connected EventStore + cleanup.
func setupPostgres(t *testing.T) (application.EventStore, func()) {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("webhook_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(pgC)
		t.Fatalf("connection string: %v", err)
	}

	db, err := sqlx.Open("pgx", connStr)
	if err != nil {
		_ = testcontainers.TerminateContainer(pgC)
		t.Fatalf("open db: %v", err)
	}

	if err := pgadapter.Migrate(ctx, db); err != nil {
		_ = db.Close()
		_ = testcontainers.TerminateContainer(pgC)
		t.Fatalf("migrate: %v", err)
	}

	store := pgadapter.NewEventStore(db)
	cleanup := func() {
		_ = db.Close()
		_ = testcontainers.TerminateContainer(pgC)
	}
	return store, cleanup
}

// --- Test cases ---

func TestE2E_HappyPath_PostAndPersist(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()

	secret := "e2e-test-secret"
	verifier, _ := webhook.NewHMACVerifier(secret)
	uc := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(uc, verifier)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	payload := []byte(`{"id":"e2e_1","source_id":"src_test","type":"order.created","payload":{"order_id":"ord_42"}}`)
	req, _ := http.NewRequest("POST", srv.URL+"/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(secret, payload))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	// Verify event persisted in real Postgres.
	ctx := context.Background()
	got, err := store.FindByID(ctx, "e2e_1")
	if err != nil {
		t.Fatalf("event not persisted: %v", err)
	}
	if got.SourceID != "src_test" || got.Type != "order.created" {
		t.Errorf("event fields wrong: %+v", got)
	}
	if string(got.Status) != "pending" {
		t.Errorf("status = %s, want pending", got.Status)
	}
}

func TestE2E_InvalidSignature_Rejected(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()

	verifier, _ := webhook.NewHMACVerifier("right-secret")
	uc := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(uc, verifier)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	payload := []byte(`{"id":"e2e_2","source_id":"src","type":"t","payload":{}}`)
	req, _ := http.NewRequest("POST", srv.URL+"/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload("WRONG-secret", payload)) // wrong!

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// Verify event NOT persisted.
	_, err = store.FindByID(context.Background(), "e2e_2")
	if err == nil {
		t.Error("event was persisted despite invalid signature")
	}
}

func TestE2E_Idempotency_SamePayloadOnce(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()

	secret := "e2e-test-secret"
	verifier, _ := webhook.NewHMACVerifier(secret)
	uc := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(uc, verifier)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Same payload sent twice — server should treat second as a duplicate
	// of the first and return the same event ID. Idempotency key is
	// (source_id, payload_hash), so different event IDs don't matter.
	//
	// IMPORTANT: signature must be over the EXACT bytes being sent. We use
	// the same `body` for both the signature and the POST body — otherwise
	// the HMAC won't match (the previous version had this bug).
	post := func(id string) int {
		body := []byte(fmt.Sprintf(`{"id":%q,"source_id":"src","type":"t","payload":{"k":"v"}}`, id))
		req, _ := http.NewRequest("POST", srv.URL+"/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signPayload(secret, body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if post("e2e_first") != http.StatusCreated {
		t.Fatalf("first POST: want 201")
	}
	if post("e2e_second") != http.StatusCreated {
		t.Fatalf("second POST: want 201 (idempotent)")
	}

	// Count events with the same hash — should be exactly 1.
	ctx := context.Background()
	all, err := store.List(ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("persisted %d events, want 1 (idempotency broken)", len(all))
	}
}

func TestE2E_Delivery_HappyPath(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Subscription target: always returns 200.
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	deliverUC := application.NewDeliverEventUseCase(infrahttp.NewPoster(time.Second),
		application.RetryPolicy{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: time.Second})

	pool := worker.NewPool(ctx, 2, 10)
	defer pool.Close()

	event := &domain.Event{
		ID:          "e2e_deliv_1",
		SourceID:    "src_test",
		Type:        "test.created",
		Payload:     []byte(`{"order_id":"ord_42"}`),
		Status:      domain.EventStatusPending,
		CreatedAt:   time.Now().UTC(),
		PayloadHash: hashPayload([]byte(`{"order_id":"ord_42"}`)),
	}
	if _, err := store.Save(ctx, event); err != nil {
		t.Fatal(err)
	}

	pool.Submit(func(_ context.Context) error {
		return deliverUC.Deliver(ctx, target.URL, event)
	})
	pool.Close()

	if got := targetCalls.Load(); got != 1 {
		t.Errorf("target called %d times, want 1", got)
	}
}

func TestE2E_Delivery_RetryThenSuccess(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Subscription: fails twice with 503, then 200.
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := targetCalls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	deliverUC := application.NewDeliverEventUseCase(infrahttp.NewPoster(time.Second),
		application.RetryPolicy{MaxAttempts: 5, BaseDelay: 10 * time.Millisecond, MaxDelay: time.Second})

	pool := worker.NewPool(ctx, 1, 10)
	defer pool.Close()

	event := &domain.Event{
		ID:          "e2e_retry_1",
		SourceID:    "src",
		Type:        "t",
		Payload:     []byte(`{}`),
		Status:      domain.EventStatusPending,
		CreatedAt:   time.Now().UTC(),
		PayloadHash: hashPayload([]byte(`{}`)),
	}
	if _, err := store.Save(ctx, event); err != nil {
		t.Fatal(err)
	}

	pool.Submit(func(_ context.Context) error {
		return deliverUC.Deliver(ctx, target.URL, event)
	})
	pool.Close()

	if got := targetCalls.Load(); got != 3 {
		t.Errorf("target called %d times, want 3 (2 retries + 1 success)", got)
	}
}

func TestE2E_Delivery_DLQAfterMaxRetries(t *testing.T) {
	store, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Subscription: always 503. Delivery should exhaust retries and return DLQ error.
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer target.Close()

	deliverUC := application.NewDeliverEventUseCase(infrahttp.NewPoster(time.Second),
		application.RetryPolicy{MaxAttempts: 3, BaseDelay: 5 * time.Millisecond, MaxDelay: time.Second})

	pool := worker.NewPool(ctx, 1, 10)
	defer pool.Close()

	event := &domain.Event{
		ID:          "e2e_dlq_1",
		SourceID:    "src",
		Type:        "t",
		Payload:     []byte(`{}`),
		Status:      domain.EventStatusPending,
		CreatedAt:   time.Now().UTC(),
		PayloadHash: hashPayload([]byte(`{}`)),
	}
	if _, err := store.Save(ctx, event); err != nil {
		t.Fatal(err)
	}

	var deliverErr error
	pool.Submit(func(_ context.Context) error {
		deliverErr = deliverUC.Deliver(ctx, target.URL, event)
		return nil
	})
	pool.Close()

	if targetCalls.Load() != 3 {
		t.Errorf("target called %d times, want 3 (max attempts)", targetCalls.Load())
	}
	if deliverErr == nil {
		t.Fatal("expected ErrMaxAttemptsExceeded, got nil")
	}
	if !strings.Contains(deliverErr.Error(), "max attempts") {
		t.Errorf("err = %v, want max attempts error", deliverErr)
	}
}

func TestE2E_FullFlow_PostReceivePersistDeliver(t *testing.T) {
	// Full happy-path: POST /events → receive use case → Save → pool delivers → target 200.
	store, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	secret := "e2e-full-secret"
	verifier, _ := webhook.NewHMACVerifier(secret)

	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	receiveUC := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(receiveUC, verifier)
	deliverUC := application.NewDeliverEventUseCase(infrahttp.NewPoster(time.Second),
		application.RetryPolicy{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: time.Second})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pool := worker.NewPool(ctx, 2, 10)
	defer pool.Close()

	payload := []byte(`{"id":"e2e_full","source_id":"src","type":"t","payload":{"k":"v"}}`)
	req, _ := http.NewRequest("POST", srv.URL+"/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(secret, payload))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST: %d", resp.StatusCode)
	}

	event, err := store.FindByID(ctx, "e2e_full")
	if err != nil {
		t.Fatalf("not persisted: %v", err)
	}

	pool.Submit(func(_ context.Context) error {
		return deliverUC.Deliver(ctx, target.URL, event)
	})
	pool.Close()

	if got := targetCalls.Load(); got != 1 {
		t.Errorf("target called %d times, want 1", got)
	}
}

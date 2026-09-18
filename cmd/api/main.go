// Package main — composition root for the webhook relay API.
//
// This file is the ONLY one that knows about all concrete types.
// It wires adapters to use cases to handlers. Use cases never know
// which adapter they're using; handlers never know which use case.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver registration
	"github.com/jmoiron/sqlx"

	"github.com/sgtour/webhook-relay/internal/application"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
	pgadapter "github.com/sgtour/webhook-relay/internal/infra/postgres"
	"github.com/sgtour/webhook-relay/internal/infra/webhook"
	"github.com/sgtour/webhook-relay/internal/infra/worker"
)

func main() {
	// --healthcheck-only exits 0 if /healthz responds 200, 1 otherwise.
	// Used by Docker HEALTHCHECK (distroless image has no curl/wget).
	healthcheckOnly := flag.Bool("healthcheck-only", false, "exit 0 if /healthz responds 200, 1 otherwise (Docker HEALTHCHECK)")
	flag.Parse()
	if *healthcheckOnly {
		resp, err := http.Get("http://localhost:8080/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		os.Exit(0)
	}

	// Structured logging to stdout in JSON format.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Root context — cancelled on SIGINT/SIGTERM. All long-lived components
	// (pool, server) inherit from this. Cleanup is graceful.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Wire the store based on DATABASE_URL.
	store := buildStore(rootCtx)
	defer store.Close()

	// Global worker pool (see Step 1.10 design decision: per-request would
	// defeat the purpose of a webhook relay — delivery must survive client
	// cancellation). Pool inherits from rootCtx so SIGTERM cancels in-flight
	// workers cleanly.
	const poolWorkers = 5
	const poolBuffer = 100
	pool := worker.NewPool(rootCtx, poolWorkers, poolBuffer)
	defer pool.Close()

	receiveEvent := application.NewReceiveEventUseCase(store)

	// Build signature verifier from WEBHOOK_SECRET env var. If unset,
	// signature verification is DISABLED — never do this in production,
	// but useful for local dev and tests.
	var verifier application.SignatureVerifier
	if secret := os.Getenv("WEBHOOK_SECRET"); secret != "" {
		v, err := webhook.NewHMACVerifier(secret)
		if err != nil {
			slog.Error("hmac verifier init failed", "error", err)
			os.Exit(1)
		}
		verifier = v
		slog.Info("hmac signature verification enabled")
	} else {
		slog.Warn("WEBHOOK_SECRET not set — signature verification DISABLED (dev mode only)")
	}

	// Wire use cases to handlers.
	handlers := infrahttp.NewEventHandlers(receiveEvent, verifier)

	// Mux with method-aware routing (Go 1.22+).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	mux.HandleFunc("POST /events", handlers.PostEvent)

	// Wrap mux with middleware. Order: RequestID runs FIRST so Logger
	// can include the ID in every log line.
	handler := infrahttp.Chain(mux,
		infrahttp.RequestID,
		infrahttp.Recovery,
		infrahttp.Logger,
	)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // prevent slowloris
	}

	// Run server in goroutine. ListenAndServe blocks; main goroutine waits
	// for signal then triggers shutdown.
	go func() {
		slog.Info("api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop() // cancel rootCtx to trigger shutdown path
		}
	}()

	// Block until SIGINT/SIGTERM (or server failure above).
	<-rootCtx.Done()
	slog.Info("shutdown signal received, draining...")

	// Phase 1: stop accepting new HTTP requests, wait for in-flight to finish.
	// In-flight handlers may Submit jobs to the pool — those need the pool
	// still alive, which is why we Close() the pool AFTER the server.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "error", err)
	} else {
		slog.Info("http server stopped cleanly")
	}

	// Phase 2: drain the worker pool. No new jobs (server stopped). Workers
	// exit on rootCtx.Done() (cancelled) or jobsCh close.
	pool.Close()
	slog.Info("worker pool drained")
}

// closableStore wraps an EventStore with a Close method for cleanup.
type closableStore struct {
	application.EventStore
	closeFn func() error
}

func (c *closableStore) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

// buildStore returns a closableStore based on DATABASE_URL.
// In-memory adapter is used when DATABASE_URL is empty (dev, tests).
func buildStore(ctx context.Context) *closableStore {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Info("DATABASE_URL not set, using in-memory store")
		return &closableStore{
			EventStore: memory.NewEventStore(),
			closeFn:    nil,
		}
	}

	slog.Info("connecting to postgres")
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		slog.Error("open db", "error", err)
		os.Exit(1)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		slog.Error("ping db", "error", err)
		os.Exit(1)
	}

	if err := pgadapter.Migrate(ctx, db); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	slog.Info("postgres ready, migrations applied")
	return &closableStore{
		EventStore: pgadapter.NewEventStore(db),
		closeFn:    db.Close,
	}
}

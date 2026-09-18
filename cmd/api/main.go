// Package main — composition root for the webhook relay API.
//
// This file is the ONLY one that knows about all concrete types.
// It wires adapters to use cases to handlers. Use cases never know
// which adapter they're using; handlers never know which use case.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver registration
	"github.com/jmoiron/sqlx"

	"github.com/sgtour/webhook-relay/internal/application"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
	pgadapter "github.com/sgtour/webhook-relay/internal/infra/postgres"
)

func main() {
	// Structured logging to stdout in JSON format. Production default.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Wire the store based on DATABASE_URL.
	// - Set: connect to Postgres, run migrations, use Postgres adapter
	// - Unset: fall back to in-memory (dev mode, tests, demos)
	ctx := context.Background()
	store := buildStore(ctx)
	defer store.Close()

	receiveEvent := application.NewReceiveEventUseCase(store)

	// Wire use cases to handlers.
	handlers := infrahttp.NewEventHandlers(receiveEvent)

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

	addr := ":8080"
	slog.Info("api listening", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
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

	// Verify connection with a ping (with timeout via ctx).
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

// Silence unused import warnings (errors is used via errors.Is in tests).
var _ = errors.Is

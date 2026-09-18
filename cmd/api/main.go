// Package main — composition root for the webhook relay API.
//
// This file is the ONLY one that knows about all concrete types.
// It wires adapters to use cases to handlers. Use cases never know
// which adapter they're using; handlers never know which use case.
package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/sgtour/webhook-relay/internal/application"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
)

func main() {
	// Structured logging to stdout in JSON format. Production default.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Wire concrete adapters to use cases.
	store := memory.NewEventStore()
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
	_ = log.Println // keep log import for now; future step removes entirely
}

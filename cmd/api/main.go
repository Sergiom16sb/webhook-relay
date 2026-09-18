// Package main — composition root for the webhook relay API.
//
// This file is the ONLY one that knows about all concrete types.
// It wires adapters to use cases to handlers. Use cases never know
// which adapter they're using; handlers never know which use case.
package main

import (
	"log"
	"net/http"

	"github.com/sgtour/webhook-relay/internal/application"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
)

func main() {
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

	addr := ":8080"
	log.Printf("api listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// Package main — composition root for the webhook relay API.
//
// Step 1.1 of the plan: bootstrap with a hello healthz endpoint.
// Future steps add handlers (POST /webhooks/:source_id), middleware, and
// dependency injection. This file stays small — only wire-up logic.
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	// Health check endpoint. Step 1.7 will replace `log` with `log/slog`.
	// `mux.HandleFunc` (not the package-level `http.HandleFunc`) keeps the
	// mux local — easier to extend in later steps without global state.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello")
	})

	addr := ":8080"
	log.Printf("api listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

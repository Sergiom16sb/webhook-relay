package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sgtour/webhook-relay/internal/application"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
)

// newTestServer wires real adapters (no mocks for handler tests).
// The handler is the integration point — wiring matters.
func newTestServer() (*httptest.Server, *memory.EventStore) {
	store := memory.NewEventStore()
	uc := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(uc)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	return httptest.NewServer(mux), store
}

func TestPostEvent_Success(t *testing.T) {
	srv, store := newTestServer()
	defer srv.Close()

	body := `{"id":"evt_1","source_id":"src_shopify","type":"order.created","payload":{"order_id":"ord_42"}}`
	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Event should be persisted via the real adapter.
	if _, err := store.FindByID(context.Background(), "evt_1"); err != nil {
		t.Errorf("event not persisted in store: %v", err)
	}

	// Response body should echo the event fields.
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["id"] != "evt_1" {
		t.Errorf("response id = %v, want evt_1", got["id"])
	}
	if got["status"] != "pending" {
		t.Errorf("response status = %v, want pending", got["status"])
	}
}

func TestPostEvent_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPostEvent_ValidationError(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty id", `{"source_id":"src","type":"t",}`},
		{"empty source", `{"id":"e1","type":"t",}`},
		{"empty type", `{"id":"e1","source_id":"src",}`},
		{"empty payload", `{"id":"e1","source_id":"src","type":"t",}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newTestServer()
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", resp.StatusCode, tt.body)
			}
		})
	}
}

func TestPostEvent_MethodNotAllowed(t *testing.T) {
	// ServeMux with method routing returns 405 for wrong methods.
	// Build a server that includes the method-restricted route.
	store := memory.NewEventStore()
	uc := application.NewReceiveEventUseCase(store)
	h := infrahttp.NewEventHandlers(uc)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// silence unused import in some toolchains
var _ = bytes.NewReader

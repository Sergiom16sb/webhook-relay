package http_test

import (
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

func TestPostEvent(t *testing.T) {
	cases := []struct {
		name           string
		method         string
		body           string
		wantStatus     int
		wantBodySubstr string // substring to check in response body; "" to skip
		wantEventSaved string // if non-empty, verify event with this ID was persisted
	}{
		{
			name:           "success persists and returns 201",
			method:         http.MethodPost,
			body:           `{"id":"evt_1","source_id":"src_shopify","type":"order.created","payload":{"order_id":"ord_42"}}`,
			wantStatus:     http.StatusCreated,
			wantBodySubstr: `"id":"evt_1"`,
			wantEventSaved: "evt_1",
		},
		{
			name:       "invalid JSON returns 400",
			method:     http.MethodPost,
			body:       `{not json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty id returns 400",
			method:     http.MethodPost,
			body:       `{"source_id":"src","type":"t","payload":{}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty source_id returns 400",
			method:     http.MethodPost,
			body:       `{"id":"e1","type":"t","payload":{}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty type returns 400",
			method:     http.MethodPost,
			body:       `{"id":"e1","source_id":"src","payload":{}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing payload returns 400",
			method:     http.MethodPost,
			body:       `{"id":"e1","source_id":"src","type":"t"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "GET on POST-only route returns 405",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, store := newTestServer()
			defer srv.Close()

			req, err := http.NewRequest(tt.method, srv.URL+"/events", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantBodySubstr != "" {
				bodyBytes, _ := readAll(resp.Body)
				if !strings.Contains(string(bodyBytes), tt.wantBodySubstr) {
					t.Errorf("body = %q, want substring %q", bodyBytes, tt.wantBodySubstr)
				}
			}

			if tt.wantEventSaved != "" {
				if _, err := store.FindByID(context.Background(), tt.wantEventSaved); err != nil {
					t.Errorf("event %q not persisted: %v", tt.wantEventSaved, err)
				}
			}
		})
	}
}

// readAll reads body without importing io in the test file.
func readAll(r interface{ Read(p []byte) (int, error) }) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}

// Silence unused import warnings if test file shrinks further.
var _ = json.RawMessage{}

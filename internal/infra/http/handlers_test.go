package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	infrahttp "github.com/Sergiom16sb/webhook-relay/internal/infra/http"
	"github.com/Sergiom16sb/webhook-relay/internal/infra/memory"
	"github.com/Sergiom16sb/webhook-relay/internal/infra/webhook"
)

const testSecret = "test-secret-key-do-not-use-in-prod"

// fakeSubmitter records submitted jobs but runs them inline (for tests
// where we don't need real worker pool semantics).
type fakeSubmitter struct{}

func (f *fakeSubmitter) Submit(job application.Job) { job(context.Background()) }

// noopExecutor is a DeliveryExecutor that does nothing — useful for tests
// where we don't care about delivery (we just want to enqueue).
type noopExecutor struct{}

func (noopExecutor) Deliver(_ context.Context, _, _ string) error { return nil }

// newTestServer wires real adapters (no mocks for handler tests).
// The handler is the integration point — wiring matters.
func newTestServer(t *testing.T) (*httptest.Server, *memory.MemoryEventStore, *webhook.HMACVerifier) {
	t.Helper()
	store := memory.NewMemoryEventStore()
	subStore := memory.NewMemorySubscriptionStore()
	attempts := memory.NewMemoryDeliveryAttemptStore()
	uc := application.NewReceiveEventUseCase(store)
	verifier, err := webhook.NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	// For tests, use a no-op executor + a fake submitter so enqueue doesn't
	// touch the worker pool or block.
	submitter := &fakeSubmitter{}
	enqueue := application.NewEnqueueDeliveriesUseCase(store, subStore, attempts, submitter, noopExecutor{})
	h := infrahttp.NewEventHandlers(uc, enqueue, verifier)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	return httptest.NewServer(mux), store, verifier
}

// newUnverifiedTestServer builds a server with signature verification
// disabled (verifier=nil). Used for tests that don't care about auth.
func newUnverifiedTestServer(t *testing.T) (*httptest.Server, *memory.MemoryEventStore) {
	t.Helper()
	store := memory.NewMemoryEventStore()
	uc := application.NewReceiveEventUseCase(store)
	submitter := &fakeSubmitter{}
	enqueue := application.NewEnqueueDeliveriesUseCase(
		store, memory.NewMemorySubscriptionStore(), memory.NewMemoryDeliveryAttemptStore(),
		submitter, noopExecutor{},
	)
	h := infrahttp.NewEventHandlers(uc, enqueue, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.PostEvent)
	return httptest.NewServer(mux), store
}

func TestPostEvent_SignatureAndValidation(t *testing.T) {
	cases := []struct {
		name           string
		body           string
		signBody       bool // whether to add valid X-Webhook-Signature
		tamperBody     bool // sign valid, then change body before sending
		wrongSig       bool // send invalid signature
		missingSig     bool // omit header entirely
		wantStatus     int
		wantBodySubstr string // substring to check in response body; "" to skip
		wantEventSaved string // if non-empty, verify event persisted
	}{
		{
			name:           "valid signature accepts and persists",
			body:           `{"id":"evt_1","source_id":"src_shopify","type":"order.created","payload":{"order_id":"ord_42"}}`,
			signBody:       true,
			wantStatus:     http.StatusCreated,
			wantBodySubstr: `"id":"evt_1"`,
			wantEventSaved: "evt_1",
		},
		{
			name:       "missing signature header returns 401",
			body:       `{"id":"evt_1","source_id":"src","type":"t","payload":{}}`,
			missingSig: true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "tampered payload with valid signature returns 401",
			body:       `{"id":"evt_2","source_id":"src","type":"t","payload":{"amount":1000}}`,
			signBody:   true,
			tamperBody: true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong signature returns 401",
			body:       `{"id":"evt_3","source_id":"src","type":"t","payload":{}}`,
			wrongSig:   true,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, store, verifier := newTestServer(t)
			defer srv.Close()

			bodyForSigning := tt.body
			if tt.tamperBody {
				// Sign the original, then change the body to mimic a tampered payload.
				bodyForSigning = `{"id":"evt_2","source_id":"src","type":"t","payload":{"amount":99999}}`
			}

			req, err := http.NewRequest(http.MethodPost, srv.URL+"/events", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")

			switch {
			case tt.missingSig:
				// no header set
			case tt.wrongSig:
				req.Header.Set("X-Webhook-Signature", "deadbeef")
			default:
				req.Header.Set("X-Webhook-Signature", verifier.Sign([]byte(bodyForSigning)))
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantEventSaved != "" {
				_, err := store.FindByID(context.Background(), tt.wantEventSaved)
				if tt.wantStatus == http.StatusCreated && err != nil {
					t.Errorf("event %q not persisted: %v", tt.wantEventSaved, err)
				}
				if tt.wantStatus != http.StatusCreated {
					// For non-2xx, the event MUST NOT be persisted.
					if err == nil {
						t.Errorf("event %q should not be persisted on auth failure", tt.wantEventSaved)
					}
				}
			}
		})
	}
}

func TestPostEvent_NoVerifier_AcceptsWithoutSignature(t *testing.T) {
	// When verifier is nil (dev mode), no signature check. All other
	// validations still apply.
	srv, _ := newUnverifiedTestServer(t)
	defer srv.Close()

	body := `{"id":"evt_dev","source_id":"src","type":"t","payload":{}}`
	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201 (no verifier = no signature check)", resp.StatusCode)
	}
}

func TestPostEvent_ValidationAndSignatureAreSeparate(t *testing.T) {
	// Signature is checked BEFORE JSON parse, so a payload with bad JSON
	// but valid signature still gets 401 (signature pass) followed by 400 (parse fail)?
	// No: signature check uses raw bytes — if signature is valid, we proceed
	// to JSON parse and may fail there. Either way, signature must be valid first.
	cases := []struct {
		name       string
		body       string
		signValid  bool
		wantStatus int
	}{
		{
			name:       "valid signature + bad JSON returns 400 (parse fails after sig pass)",
			body:       `{not json`,
			signValid:  true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid signature + bad JSON returns 401 (sig fails first)",
			body:       `{not json`,
			signValid:  false,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, verifier := newTestServer(t)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/events", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.signValid {
				req.Header.Set("X-Webhook-Signature", verifier.Sign([]byte(tt.body)))
			} else {
				req.Header.Set("X-Webhook-Signature", "deadbeef")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("got %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

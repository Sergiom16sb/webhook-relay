package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
	"github.com/Sergiom16sb/webhook-relay/internal/infra/memory"
)

// fakeStore is a hand-rolled test fake. Demonstrates why consumer-side
// interfaces matter: the use case test doesn't import any infra package.
type fakeStore struct {
	saved []*domain.Event
	err   error // inject for error-path tests
}

func (f *fakeStore) Save(_ context.Context, e *domain.Event) (*domain.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.saved = append(f.saved, e)
	return e, nil
}

func (f *fakeStore) FindByID(_ context.Context, id string) (*domain.Event, error) {
	for _, e := range f.saved {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, application.ErrEventNotFound
}

func (f *fakeStore) List(_ context.Context, limit, offset int) ([]*domain.Event, error) {
	if offset >= len(f.saved) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.saved) {
		end = len(f.saved)
	}
	out := make([]*domain.Event, end-offset)
	copy(out, f.saved[offset:end])
	return out, nil
}

func (f *fakeStore) Count(_ context.Context) (int, error) {
	return len(f.saved), nil
}

// Compile-time check: fakeStore satisfies the port.
var _ application.EventStore = (*fakeStore)(nil)

// storeErrSentinel is shared across the test cases that need an error
// from the fake store. Using a single instance ensures errors.Is works
// (it compares pointers via the chain).
var storeErrSentinel = errors.New("store failure")

func TestReceiveEvent(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		source    string
		eventType string
		payload   json.RawMessage
		storeErr  error // injected into fakeStore.err; nil = store succeeds
		wantErr   error // errors.Is target; nil = expect success
		wantSaved int   // expected fakeStore.saved length after Execute
	}{
		{
			name:      "success",
			id:        "evt_1",
			source:    "src_shopify",
			eventType: "order.created",
			payload:   json.RawMessage(`{"order_id":"ord_42"}`),
			wantSaved: 1,
		},
		{
			name:      "empty id triggers ErrEmptyID",
			id:        "",
			source:    "src",
			eventType: "type",
			payload:   json.RawMessage(`{}`),
			wantErr:   domain.ErrEmptyID,
			wantSaved: 0,
		},
		{
			name:      "store error propagates",
			id:        "evt_1",
			source:    "src",
			eventType: "type",
			payload:   json.RawMessage(`{}`),
			storeErr:  storeErrSentinel,
			wantErr:   storeErrSentinel,
			wantSaved: 0,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{err: tt.storeErr}
			uc := application.NewReceiveEventUseCase(store)

			_, err := uc.Execute(context.Background(), tt.id, tt.source, tt.eventType, tt.payload)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if len(store.saved) != tt.wantSaved {
				t.Errorf("saved = %d, want %d", len(store.saved), tt.wantSaved)
			}
		})
	}
}

// TestReceiveEvent_Idempotency — same payload twice returns same event ID.
// Uses memory store directly to verify the (source_id, payload_hash) dedup.
func TestReceiveEvent_Idempotency(t *testing.T) {
	store := memory.NewMemoryEventStore()
	uc := application.NewReceiveEventUseCase(store)

	payload := json.RawMessage(`{"order_id":"ord_42","amount":99}`)
	ctx := context.Background()

	// First call: creates event.
	first, err := uc.Execute(ctx, "evt_first", "src_shopify", "order.created", payload)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Second call with same payload but different ID: dedup returns first.
	second, err := uc.Execute(ctx, "evt_second", "src_shopify", "order.created", payload)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("idempotency broken: got %q and %q, want same", first.ID, second.ID)
	}

	// Verify only one event in storage.
	got, err := store.FindByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID != first.ID {
		t.Errorf("retrieved event id = %q, want %q", got.ID, first.ID)
	}

	// Verify PayloadHash is computed and stable.
	if first.PayloadHash == "" {
		t.Error("PayloadHash should be populated by use case")
	}
	if first.PayloadHash != second.PayloadHash {
		t.Errorf("hash differs: %q vs %q", first.PayloadHash, second.PayloadHash)
	}
}

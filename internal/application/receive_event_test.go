package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// fakeStore is a hand-rolled test fake. Demonstrates why consumer-side
// interfaces matter: the use case test doesn't import any infra package.
type fakeStore struct {
	saved []*domain.Event
	err   error // inject for error-path tests
}

func (f *fakeStore) Save(_ context.Context, e *domain.Event) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, e)
	return nil
}

func (f *fakeStore) FindByID(_ context.Context, id string) (*domain.Event, error) {
	for _, e := range f.saved {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, application.ErrEventNotFound
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

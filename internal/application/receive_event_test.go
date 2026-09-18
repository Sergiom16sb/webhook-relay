package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sgtour/webhook-platform-go/internal/application"
	"github.com/sgtour/webhook-platform-go/internal/domain"
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

func TestReceiveEvent_Success(t *testing.T) {
	store := &fakeStore{}
	uc := application.NewReceiveEventUseCase(store)

	payload := json.RawMessage(`{"order_id":"ord_42"}`)
	got, err := uc.Execute(context.Background(), "evt_1", "src_shopify", "order.created", payload)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got.ID != "evt_1" {
		t.Errorf("got ID=%q, want evt_1", got.ID)
	}
	if got.Status != domain.EventStatusPending {
		t.Errorf("got status=%q, want pending", got.Status)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved event, got %d", len(store.saved))
	}
}

func TestReceiveEvent_ValidationError(t *testing.T) {
	store := &fakeStore{}
	uc := application.NewReceiveEventUseCase(store)

	// Empty ID triggers ErrEmptyID — store must NOT be called.
	_, err := uc.Execute(context.Background(), "", "src", "type", json.RawMessage(`{}`))
	if !errors.Is(err, domain.ErrEmptyID) {
		t.Fatalf("got %v, want ErrEmptyID", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("store should not be called on validation error, got %d saved", len(store.saved))
	}
}

func TestReceiveEvent_StoreError(t *testing.T) {
	injected := errors.New("db exploded")
	store := &fakeStore{err: injected}
	uc := application.NewReceiveEventUseCase(store)

	_, err := uc.Execute(context.Background(), "evt_1", "src", "type", json.RawMessage(`{}`))
	if !errors.Is(err, injected) {
		t.Fatalf("got %v, want %v", err, injected)
	}
}

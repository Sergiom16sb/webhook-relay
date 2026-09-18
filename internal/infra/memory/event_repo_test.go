package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sgtour/webhook-platform-go/internal/application"
	"github.com/sgtour/webhook-platform-go/internal/domain"
	"github.com/sgtour/webhook-platform-go/internal/infra/memory"
)

// validEvent returns an Event that passes Validate.
func validEvent(id string) *domain.Event {
	return &domain.Event{
		ID:        id,
		SourceID:  "src_test",
		Type:      "test.created",
		Payload:   json.RawMessage(`{"ok":true}`),
		Status:    domain.EventStatusPending,
		CreatedAt: mustParseTime("2026-09-17T22:00:00Z"),
	}
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEventStore_SaveAndFindByID(t *testing.T) {
	store := memory.NewEventStore()
	ctx := context.Background()

	e := validEvent("evt_1")
	if err := store.Save(ctx, e); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.FindByID(ctx, "evt_1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID != "evt_1" {
		t.Errorf("got ID=%q, want evt_1", got.ID)
	}
	if got.Status != domain.EventStatusPending {
		t.Errorf("got status=%q, want pending", got.Status)
	}
}

func TestEventStore_FindByID_NotFound(t *testing.T) {
	store := memory.NewEventStore()
	ctx := context.Background()

	_, err := store.FindByID(ctx, "missing")
	if !errors.Is(err, application.ErrEventNotFound) {
		t.Fatalf("got %v, want ErrEventNotFound", err)
	}
}

func TestEventStore_Overwrite(t *testing.T) {
	store := memory.NewEventStore()
	ctx := context.Background()

	e1 := validEvent("evt_1")
	e1.Status = domain.EventStatusPending
	if err := store.Save(ctx, e1); err != nil {
		t.Fatal(err)
	}

	e2 := validEvent("evt_1")
	e2.Status = domain.EventStatusDelivered
	if err := store.Save(ctx, e2); err != nil {
		t.Fatal(err)
	}

	got, _ := store.FindByID(ctx, "evt_1")
	if got.Status != domain.EventStatusDelivered {
		t.Errorf("got status=%q, want delivered (overwritten)", got.Status)
	}
}

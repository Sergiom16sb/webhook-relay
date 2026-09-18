package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
	"github.com/sgtour/webhook-relay/internal/infra/memory"
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

// deliveredEvent returns a valid event with status=Delivered.
func deliveredEvent(id string) *domain.Event {
	e := validEvent(id)
	e.Status = domain.EventStatusDelivered
	return e
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEventStore(t *testing.T) {
	cases := []struct {
		name       string
		seed       []*domain.Event // pre-populate store
		save       *domain.Event   // event to save; nil = skip save
		query      string          // id to find after save; "" = skip find
		wantErr    error           // expected error from find (or from save if save fails)
		wantStatus domain.EventStatus
	}{
		{
			name:       "save then find returns the event",
			save:       validEvent("evt_1"),
			query:      "evt_1",
			wantStatus: domain.EventStatusPending,
		},
		{
			name:    "find missing returns ErrEventNotFound",
			query:   "missing",
			wantErr: application.ErrEventNotFound,
		},
		{
			name:       "save overwrites existing id",
			seed:       []*domain.Event{validEvent("evt_1")},
			save:       deliveredEvent("evt_1"),
			query:      "evt_1",
			wantStatus: domain.EventStatusDelivered,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := memory.NewEventStore()
			ctx := context.Background()

			for _, e := range tt.seed {
				if err := store.Save(ctx, e); err != nil {
					t.Fatalf("seed save: %v", err)
				}
			}

			if tt.save != nil {
				if err := store.Save(ctx, tt.save); err != nil {
					t.Fatalf("save: %v", err)
				}
			}

			if tt.query != "" {
				got, err := store.FindByID(ctx, tt.query)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				if err == nil && got.Status != tt.wantStatus {
					t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
				}
			}
		})
	}
}

// Package memory — regression test for the single-row state machine upsert.
//
// Bug fixed: prior to 0004, the executor and enqueue use case used
// different ID functions, so a Save(pending) + Save(success) for the
// same (event, sub) created TWO rows. The dashboard would show a ghost
// pending row forever. This test guards the upsert contract.
package memory

import (
	"context"
	"testing"
	"time"

	"github.com/sgtour/webhook-relay/internal/domain"
)

func TestMemoryDeliveryAttemptStore_UpsertReplaces(t *testing.T) {
	store := NewMemoryDeliveryAttemptStore()
	now := time.Now().UTC()

	pending := &domain.DeliveryAttempt{
		ID:             "shared-id",
		EventID:        "evt_1",
		SubscriptionID: "sub_1",
		AttemptNum:     1,
		AttemptCount:   1,
		Status:         domain.DeliveryAttemptPending,
		StartedAt:      now,
	}
	if _, err := store.Save(context.Background(), pending); err != nil {
		t.Fatalf("save pending: %v", err)
	}

	completed := now.Add(time.Second)
	success := &domain.DeliveryAttempt{
		ID:             "shared-id", // same ID — upsert semantics
		EventID:        "evt_1",
		SubscriptionID: "sub_1",
		AttemptNum:     1,
		AttemptCount:   1,
		Status:         domain.DeliveryAttemptSuccess,
		StartedAt:      now,
		CompletedAt:    &completed,
	}
	if _, err := store.Save(context.Background(), success); err != nil {
		t.Fatalf("save success: %v", err)
	}

	all, err := store.ListByEventID(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("rows = %d, want 1 (state machine: one row per event+sub)", len(all))
	}
	if all[0].Status != domain.DeliveryAttemptSuccess {
		t.Errorf("status = %q, want success (latest write wins)", all[0].Status)
	}
}

func TestMemoryDeliveryAttemptStore_OneRowPerSubscription(t *testing.T) {
	// Two subscriptions for the same event should produce two rows.
	store := NewMemoryDeliveryAttemptStore()
	now := time.Now().UTC()

	for _, subID := range []string{"sub_a", "sub_b"} {
		_, err := store.Save(context.Background(), &domain.DeliveryAttempt{
			ID:             subID,
			EventID:        "evt_1",
			SubscriptionID: subID,
			AttemptNum:     1,
			AttemptCount:   1,
			Status:         domain.DeliveryAttemptPending,
			StartedAt:      now,
		})
		if err != nil {
			t.Fatalf("save %s: %v", subID, err)
		}
	}

	all, err := store.ListByEventID(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("rows = %d, want 2 (one per subscription)", len(all))
	}
}

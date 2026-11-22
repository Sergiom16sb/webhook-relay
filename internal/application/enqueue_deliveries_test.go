package application_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
	"github.com/Sergiom16sb/webhook-relay/internal/infra/memory"
)

// --- Test fakes ---

// fakeJobSubmitter captures submitted closures for later inspection.
type fakeJobSubmitter struct {
	mu   sync.Mutex
	jobs []application.Job
}

func (f *fakeJobSubmitter) Submit(job application.Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, job)
}

// fakeExecutor is a no-op DeliveryExecutor — these tests only assert enqueue
// behavior (job submission, attempt creation), never delivery itself.
type fakeExecutor struct{}

func (fakeExecutor) Deliver(_ context.Context, _, _ string) error { return nil }

func (f *fakeJobSubmitter) runAll(ctx context.Context) {
	f.mu.Lock()
	jobs := append([]application.Job{}, f.jobs...)
	f.mu.Unlock()
	for _, j := range jobs {
		_ = j(ctx)
	}
}

// --- Tests ---

func TestEnqueueDeliveries_NoSubscriptions(t *testing.T) {
	eventStore := memory.NewMemoryEventStore()
	subStore := memory.NewMemorySubscriptionStore()
	attempts := memory.NewMemoryDeliveryAttemptStore()
	submitter := &fakeJobSubmitter{}

	uc := application.NewEnqueueDeliveriesUseCase(eventStore, subStore, attempts, submitter, fakeExecutor{})

	// Need an event to exist first.
	event := &domain.Event{
		ID:        "evt_1",
		SourceID:  "src_no_subs",
		Type:      "test.created",
		Payload:   json.RawMessage(`{}`),
		Status:    domain.EventStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	eventStore.Save(context.Background(), event)

	n, err := uc.Enqueue(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if n != 0 {
		t.Errorf("enqueued = %d, want 0", n)
	}
	if len(submitter.jobs) != 0 {
		t.Errorf("jobs = %d, want 0", len(submitter.jobs))
	}
}

func TestEnqueueDeliveries_ActiveAndDeleted(t *testing.T) {
	eventStore := memory.NewMemoryEventStore()
	subStore := memory.NewMemorySubscriptionStore()
	attempts := memory.NewMemoryDeliveryAttemptStore()
	submitter := &fakeJobSubmitter{}

	// Seed: one active, one deleted sub.
	activeSub := &domain.Subscription{
		ID:        "sub_active",
		SourceID:  "src",
		TargetURL: "https://x.com/wh",
		Secret:    "s",
		Status:    domain.SubscriptionStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	deletedSub := &domain.Subscription{
		ID:        "sub_deleted",
		SourceID:  "src",
		TargetURL: "https://y.com/wh",
		Secret:    "s",
		Status:    domain.SubscriptionStatusDeleted,
		CreatedAt: time.Now().UTC(),
	}
	subStore.Save(context.Background(), activeSub)
	subStore.Save(context.Background(), deletedSub)

	// Seed event.
	event := &domain.Event{
		ID:        "evt_1",
		SourceID:  "src",
		Type:      "test.created",
		Payload:   json.RawMessage(`{}`),
		Status:    domain.EventStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	eventStore.Save(context.Background(), event)

	uc := application.NewEnqueueDeliveriesUseCase(eventStore, subStore, attempts, submitter, fakeExecutor{})

	n, err := uc.Enqueue(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if n != 1 {
		t.Errorf("enqueued = %d, want 1 (only active sub should be picked)", n)
	}
	if len(submitter.jobs) != 1 {
		t.Errorf("jobs = %d, want 1", len(submitter.jobs))
	}
}

func TestEnqueueDeliveries_CreatesPendingAttempt(t *testing.T) {
	eventStore := memory.NewMemoryEventStore()
	subStore := memory.NewMemorySubscriptionStore()
	attempts := memory.NewMemoryDeliveryAttemptStore()
	submitter := &fakeJobSubmitter{}

	activeSub := &domain.Subscription{
		ID:        "sub_1",
		SourceID:  "src",
		TargetURL: "https://x.com/wh",
		Secret:    "s",
		Status:    domain.SubscriptionStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	subStore.Save(context.Background(), activeSub)

	event := &domain.Event{
		ID:        "evt_1",
		SourceID:  "src",
		Type:      "test.created",
		Payload:   json.RawMessage(`{}`),
		Status:    domain.EventStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	eventStore.Save(context.Background(), event)

	uc := application.NewEnqueueDeliveriesUseCase(eventStore, subStore, attempts, submitter, fakeExecutor{})
	_, _ = uc.Enqueue(context.Background(), "evt_1")

	// Verify attempt was created.
	ctx := context.Background()
	allAttempts, err := attempts.ListByEventID(ctx, "evt_1")
	if err != nil {
		t.Fatalf("ListByEventID: %v", err)
	}
	if len(allAttempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(allAttempts))
	}
	a := allAttempts[0]
	if a.Status != domain.DeliveryAttemptPending {
		t.Errorf("status = %q, want pending", a.Status)
	}
	if a.AttemptNum != 1 {
		t.Errorf("attempt_num = %d, want 1", a.AttemptNum)
	}
	if a.SubscriptionID != "sub_1" {
		t.Errorf("subscription_id = %q, want sub_1", a.SubscriptionID)
	}
}

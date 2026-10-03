package application

import (
	"context"
	"fmt"
	"time"

	"github.com/sgtour/webhook-relay/internal/domain"
)

// Job is the function signature of a unit of work the worker pool runs.
// This MUST match worker.Job (an alias) so the pool satisfies this interface
// implicitly across packages.
type Job = func(ctx context.Context) error

// JobSubmitter is the port the use case needs to schedule delivery work.
// Adapters (worker.Pool) implement this — typing estructural.
type JobSubmitter interface {
	Submit(job Job)
}

// DeliveryExecutor is what the worker closure calls to actually deliver.
// The application layer doesn't know about HTTP / retries — the wiring
// layer (main.go) supplies a concrete DeliveryExecutor (typically
// wrapping DeliverEventUseCase with retry + backoff).
type DeliveryExecutor interface {
	Deliver(ctx context.Context, eventID, subscriptionID string) error
}

// EnqueueDeliveriesUseCase finds subscriptions matching an event and submits
// a delivery job for each. Creates one initial DeliveryAttempt record
// (status=pending) per subscription.
type EnqueueDeliveriesUseCase struct {
	events    EventStore
	subs      SubscriptionStore
	attempts  DeliveryAttemptStore
	submitter JobSubmitter
	executor  DeliveryExecutor
}

// NewEnqueueDeliveriesUseCase constructs the use case.
// `submitter` is worker.Pool — wraps Submit().
// `executor` performs the actual delivery — wired by main.go.
func NewEnqueueDeliveriesUseCase(
	events EventStore,
	subs SubscriptionStore,
	attempts DeliveryAttemptStore,
	submitter JobSubmitter,
	executor DeliveryExecutor,
) *EnqueueDeliveriesUseCase {
	return &EnqueueDeliveriesUseCase{
		events:    events,
		subs:      subs,
		attempts:  attempts,
		submitter: submitter,
		executor:  executor,
	}
}

// Enqueue looks up active subscriptions for the event's source_id and
// submits a delivery job for each. Returns the number of jobs enqueued.
func (uc *EnqueueDeliveriesUseCase) Enqueue(ctx context.Context, eventID string) (int, error) {
	event, err := uc.events.FindByID(ctx, eventID)
	if err != nil {
		return 0, fmt.Errorf("enqueue: lookup event: %w", err)
	}

	subs, err := uc.subs.ListBySourceID(ctx, event.SourceID)
	if err != nil {
		return 0, fmt.Errorf("enqueue: list subscriptions: %w", err)
	}

	enqueued := 0
	for _, sub := range subs {
		if sub.Status != domain.SubscriptionStatusActive {
			continue
		}
		eid, sid := eventID, sub.ID

		_, err := uc.attempts.Save(ctx, &domain.DeliveryAttempt{
			ID:             domain.AttemptID(eid, sid),
			EventID:        eid,
			SubscriptionID: sid,
			AttemptNum:     1,
			AttemptCount:   1,
			Status:         domain.DeliveryAttemptPending,
			StartedAt:      time.Now().UTC(),
		})
		if err != nil {
			return enqueued, fmt.Errorf("enqueue: save attempt: %w", err)
		}

		// Submit a job to the worker. The closure invokes the executor
		// (wired in main.go) which performs the HTTP POST + retry + records
		// the result. Application layer stays clean.
		uc.submitter.Submit(func(jobCtx context.Context) error {
			return uc.executor.Deliver(jobCtx, eid, sid)
		})
		enqueued++
	}

	return enqueued, nil
}

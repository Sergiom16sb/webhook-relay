// Package worker — delivery executor adapter.
//
// Implements application.DeliveryExecutor. Wires the existing delivery
// use case (with retry + backoff) to the worker pool's job invocation.
package worker

import (
	"context"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// DeliveryExecutor wires the worker pool's job invocation to the delivery
// use case. Implements application.DeliveryExecutor.
type DeliveryExecutor struct {
	delivery *application.DeliverEventUseCase
	events   application.EventStore
	subs     application.SubscriptionStore
	attempts application.DeliveryAttemptStore
}

// NewDeliveryExecutor constructs the executor.
func NewDeliveryExecutor(
	delivery *application.DeliverEventUseCase,
	events application.EventStore,
	subs application.SubscriptionStore,
	attempts application.DeliveryAttemptStore,
) *DeliveryExecutor {
	return &DeliveryExecutor{
		delivery: delivery,
		events:   events,
		subs:     subs,
		attempts: attempts,
	}
}

// Deliver performs the actual webhook delivery with retry. Called by the
// worker pool when an event needs delivery to a subscription.
//
// Updates the (event, sub) attempt row to Success (or Failed if the
// delivery use case returned an error). Uses the SAME attempt ID as
// EnqueueDeliveriesUseCase (domain.AttemptID) so the row is upserted
// in place, not duplicated.
func (e *DeliveryExecutor) Deliver(ctx context.Context, eventID, subscriptionID string) error {
	event, err := e.events.FindByID(ctx, eventID)
	if err != nil {
		return err
	}
	sub, err := e.subs.FindByID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	// Run the existing delivery use case (with retry + backoff).
	deliverErr := e.delivery.Deliver(ctx, sub.TargetURL, event)

	// Upsert the attempt row to reflect the outcome.
	completed := time.Now().UTC()
	status := domain.DeliveryAttemptSuccess
	if deliverErr != nil {
		status = domain.DeliveryAttemptFailed
	}
	_, _ = e.attempts.Save(ctx, &domain.DeliveryAttempt{
		ID:             domain.AttemptID(eventID, subscriptionID),
		EventID:        eventID,
		SubscriptionID: subscriptionID,
		AttemptNum:     1,
		AttemptCount:   1,
		Status:         status,
		Error:          errMsg(deliverErr),
		CompletedAt:    &completed,
	})

	return nil
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sgtour/webhook-platform-go/internal/domain"
)

// ReceiveEventUseCase handles incoming webhook events.
// Depends ONLY on the EventStore port — never on a concrete adapter.
// This is what enables testing with a fake store (no DB, no goroutines).
type ReceiveEventUseCase struct {
	store EventStore
}

// NewReceiveEventUseCase constructs the use case with the injected store.
func NewReceiveEventUseCase(store EventStore) *ReceiveEventUseCase {
	return &ReceiveEventUseCase{store: store}
}

// Execute persists a new event with the given ID, source, type, and payload.
// The ID is caller-provided so the use case stays focused on persistence.
// (UUID generation will move into a Clock/IDGenerator port later if needed.)
//
// Returns the persisted event (with CreatedAt populated) or a validation
// error from domain.Validate.
func (uc *ReceiveEventUseCase) Execute(
	ctx context.Context,
	id, sourceID, eventType string,
	payload json.RawMessage,
) (*domain.Event, error) {
	e := &domain.Event{
		ID:        id,
		SourceID:  sourceID,
		Type:      eventType,
		Payload:   payload,
		Status:    domain.EventStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if err := uc.store.Save(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

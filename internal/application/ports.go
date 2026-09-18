// Package application holds use cases and the ports they declare.
// Imports domain/ + stdlib. NEVER imports infra/.
package application

import (
	"context"

	"github.com/sgtour/webhook-platform-go/internal/domain"
)

// EventStore is the port the application needs to persist events.
// Adapters in internal/infra/ implement this interface — typing estructural
// makes the binding automatic; no `implements` keyword needed.
type EventStore interface {
	// Save persists an event. Returns ErrEventNotFound if the store rejects
	// a duplicate ID (caller's choice — depends on idempotency strategy).
	Save(ctx context.Context, e *domain.Event) error

	// FindByID returns the event for the given ID. Returns ErrEventNotFound
	// if no event with that ID exists.
	FindByID(ctx context.Context, id string) (*domain.Event, error)
}

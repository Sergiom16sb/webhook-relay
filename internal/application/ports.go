// Package application holds use cases and the ports they declare.
// Imports domain/ + stdlib. NEVER imports infra/.
package application

import (
	"context"

	"github.com/sgtour/webhook-relay/internal/domain"
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

// SignatureVerifier verifies a signature over a payload using a secret.
// The adapter (HMAC, RSA, etc.) is chosen in infra; the use case only
// sees this interface and doesn't care about the algorithm.
//
// Verify returns nil if the signature is valid, ErrInvalidSignature
// otherwise. Other errors (malformed input, missing secret) should be
// surfaced distinctly so the handler can return 400 vs 401.
type SignatureVerifier interface {
	Verify(payload []byte, signature string) error
}

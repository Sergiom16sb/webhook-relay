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
	// Save persists an event idempotently by (source_id, payload_hash).
	// Returns the persisted event (whether newly created or pre-existing).
	// On idempotent retry, the SAME event ID is returned both times.
	Save(ctx context.Context, e *domain.Event) (*domain.Event, error)

	// FindByID returns the event for the given ID. Returns ErrEventNotFound
	// if no event with that ID exists.
	FindByID(ctx context.Context, id string) (*domain.Event, error)

	// List returns a page of events, ordered by created_at DESC.
	// limit: max events per page. offset: events to skip.
	// Empty list + nil error if no events match.
	List(ctx context.Context, limit, offset int) ([]*domain.Event, error)

	// Count returns the total number of events (no pagination).
	Count(ctx context.Context) (int, error)
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

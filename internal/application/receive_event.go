package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/domain"
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
// Returns the persisted event (with CreatedAt and PayloadHash populated) or
// a validation error from domain.Validate.
//
// Idempotency: PayloadHash is computed from raw Payload bytes. Same
// (source_id, payload_hash) = duplicate → store returns existing event
// instead of inserting a new one. The caller (handler) gets the existing
// event ID back either way.
func (uc *ReceiveEventUseCase) Execute(
	ctx context.Context,
	id, sourceID, eventType string,
	payload json.RawMessage,
) (*domain.Event, error) {
	e := &domain.Event{
		ID:          id,
		SourceID:    sourceID,
		Type:        eventType,
		Payload:     payload,
		Status:      domain.EventStatusPending,
		CreatedAt:   time.Now().UTC(),
		PayloadHash: hashPayload(payload),
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	saved, err := uc.store.Save(ctx, e)
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// hashPayload computes sha256 of the raw payload bytes.
// Note: whitespace differences in JSON produce different hashes. For true
// cross-format idempotency, callers should canonicalize JSON before sending
// (e.g., minify + sort keys). For v1, byte-exact match is enough.
func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

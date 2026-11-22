package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// ErrSubscriptionNotFound — returned when a subscription lookup fails.
var ErrSubscriptionNotFound = errors.New("subscription not found")

// CreateSubscriptionUseCase handles admin-initiated subscription creation.
// The ID is generated server-side (random 16 bytes hex = 32 chars).
// Status defaults to "active".
type CreateSubscriptionUseCase struct {
	store SubscriptionStore
}

// NewCreateSubscriptionUseCase constructs the use case.
func NewCreateSubscriptionUseCase(store SubscriptionStore) *CreateSubscriptionUseCase {
	return &CreateSubscriptionUseCase{store: store}
}

// Create persists a new subscription. Returns the persisted row (with ID +
// CreatedAt populated).
func (uc *CreateSubscriptionUseCase) Create(ctx context.Context, sourceID, targetURL, secret string) (*domain.Subscription, error) {
	sub := &domain.Subscription{
		ID:        generateID(),
		SourceID:  sourceID,
		TargetURL: targetURL,
		Secret:    secret,
		Status:    domain.SubscriptionStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := sub.Validate(); err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return uc.store.Save(ctx, sub)
}

// generateID returns 32 hex chars (16 random bytes).
func generateID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// --- Delete (mark as deleted) ---

// DeleteSubscriptionUseCase soft-deletes a subscription (status = "deleted").
// The row stays in the table so historical delivery attempts remain referencable.
type DeleteSubscriptionUseCase struct {
	store SubscriptionStore
}

func NewDeleteSubscriptionUseCase(store SubscriptionStore) *DeleteSubscriptionUseCase {
	return &DeleteSubscriptionUseCase{store: store}
}

// Delete marks a subscription as deleted by ID. Returns
// ErrSubscriptionNotFound if no row with that ID exists.
func (uc *DeleteSubscriptionUseCase) Delete(ctx context.Context, id string) error {
	sub, err := uc.store.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			return err
		}
		return err
	}
	sub.Status = domain.SubscriptionStatusDeleted
	_, err = uc.store.Save(ctx, sub)
	return err
}

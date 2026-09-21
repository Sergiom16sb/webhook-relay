// Package domain holds the enterprise rules: pure entities, value objects,
// and domain errors. NO imports of application/, infra/, http/, sql/, or any
// non-stdlib non-pure package.
package domain

import (
	"errors"
	"time"
)

// SubscriptionStatus tracks whether a subscription is active.
type SubscriptionStatus string

const (
	SubscriptionStatusActive  SubscriptionStatus = "active"
	SubscriptionStatusPaused  SubscriptionStatus = "paused"
	SubscriptionStatusDeleted SubscriptionStatus = "deleted"
)

// Subscription represents a destination where events for a source are delivered.
// One subscription per (source_id, target_url) — duplicates are rejected
// at the application layer.
type Subscription struct {
	ID        string
	SourceID  string            // matches Event.SourceID for routing
	TargetURL string            // where the event is POSTed
	Secret    string            // HMAC secret for signing the delivery
	Status    SubscriptionStatus
	CreatedAt time.Time
}

// Validate checks required fields. Returns first error or nil.
func (s *Subscription) Validate() error {
	if s.ID == "" {
		return ErrEmptyID
	}
	if s.SourceID == "" {
		return ErrEmptySourceID
	}
	if s.TargetURL == "" {
		return errors.New("subscription: target URL is required")
	}
	if s.Secret == "" {
		return errors.New("subscription: secret is required for HMAC signing")
	}
	if s.Status == "" {
		return errors.New("subscription: status is required")
	}
	return nil
}

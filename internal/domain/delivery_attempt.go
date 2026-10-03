package domain

import "time"

// DeliveryAttemptStatus tracks the outcome of one delivery attempt.
type DeliveryAttemptStatus string

const (
	DeliveryAttemptPending  DeliveryAttemptStatus = "pending"
	DeliveryAttemptSuccess  DeliveryAttemptStatus = "success"
	DeliveryAttemptFailed   DeliveryAttemptStatus = "failed"
	DeliveryAttemptRetrying DeliveryAttemptStatus = "retrying"
)

// DeliveryAttempt records the delivery state of an event to a subscription.
// One row per (event, subscription). Status transitions:
//
//	pending → success | failed | retrying
//
// Append-only retry history is intentionally NOT modeled here — see
// internal/domain/attempt_id.go for the single-row state machine rationale.
type DeliveryAttempt struct {
	ID             string
	EventID        string
	SubscriptionID string
	AttemptNum     int // vestigial: always 1 in the state-machine design
	AttemptCount   int // forward-looking: how many HTTP POSTs were made
	Status         DeliveryAttemptStatus
	StatusCode     int    // HTTP response code from target (0 if no response)
	Error          string // error message if Status == failed
	StartedAt      time.Time
	CompletedAt    *time.Time
}

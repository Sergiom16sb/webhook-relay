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

// DeliveryAttempt records one attempt to deliver an event to a subscription.
// Multiple attempts per (event, subscription) — indexed by AttemptNum.
type DeliveryAttempt struct {
	ID             string
	EventID        string
	SubscriptionID string
	AttemptNum     int
	Status         DeliveryAttemptStatus
	StatusCode     int    // HTTP response code from target (0 if no response)
	Error          string // error message if Status == failed
	StartedAt      time.Time
	CompletedAt    *time.Time
}

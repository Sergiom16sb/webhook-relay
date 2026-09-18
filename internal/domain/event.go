// Package domain holds the enterprise rules: pure entities, value objects,
// and domain errors. NO imports of application/, infra/, http/, sql/, or any
// non-stdlib non-pure package.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// EventStatus tracks where an event is in its lifecycle.
type EventStatus string

const (
	EventStatusPending   EventStatus = "pending"
	EventStatusDelivered EventStatus = "delivered"
	EventStatusFailed    EventStatus = "failed"
)

// IsTerminal returns true for states that don't change.
func (s EventStatus) IsTerminal() bool {
	return s == EventStatusDelivered || s == EventStatusFailed
}

// Event is the central entity of the webhook relay. Receivers create
// Events from incoming HTTP requests; the worker pool picks them up and
// attempts delivery to subscriptions.
type Event struct {
	ID        string          `json:"id"`
	SourceID  string          `json:"source_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Status    EventStatus     `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
}

// Sentinel errors for Event validation. Use errors.Is to detect.
var (
	ErrEmptyID       = errors.New("event id is required")
	ErrEmptySourceID = errors.New("source id is required")
	ErrEmptyType     = errors.New("event type is required")
	ErrEmptyPayload  = errors.New("event payload is required")
)

// Validate checks required fields. Returns nil if valid, first error otherwise.
// Business rule: an event without id/source_id/type/payload is meaningless
// and should never reach the worker pool.
func (e *Event) Validate() error {
	if e.ID == "" {
		return ErrEmptyID
	}
	if e.SourceID == "" {
		return ErrEmptySourceID
	}
	if e.Type == "" {
		return ErrEmptyType
	}
	if len(e.Payload) == 0 {
		return ErrEmptyPayload
	}
	return nil
}

// String implements fmt.Stringer for logging.
func (e *Event) String() string {
	return fmt.Sprintf("Event{id=%s, source=%s, type=%s, status=%s}",
		e.ID, e.SourceID, e.Type, e.Status)
}

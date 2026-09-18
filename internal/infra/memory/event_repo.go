// Package memory provides an in-memory adapter for application.EventStore.
// Useful for tests, dev mode, and small workloads where persistence
// doesn't matter. For production, replace with internal/infra/postgres.
package memory

import (
	"context"
	"sync"

	"github.com/sgtour/webhook-platform-go/internal/application"
	"github.com/sgtour/webhook-platform-go/internal/domain"
)

// Compile-time check: this struct satisfies application.EventStore.
// If you remove or rename a method on the port, build fails here.
var _ application.EventStore = (*EventStore)(nil)

// EventStore is the in-memory implementation of the EventStore port.
// RWMutex: many concurrent reads OK, writes exclusive.
type EventStore struct {
	mu   sync.RWMutex
	data map[string]*domain.Event
}

// NewEventStore returns an initialized EventStore.
func NewEventStore() *EventStore {
	return &EventStore{data: make(map[string]*domain.Event)}
}

// Save stores the event. Overwrites if ID exists (callers control idempotency).
func (s *EventStore) Save(_ context.Context, e *domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[e.ID] = e
	return nil
}

// FindByID returns the event for the given ID.
func (s *EventStore) FindByID(_ context.Context, id string) (*domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[id]
	if !ok {
		return nil, application.ErrEventNotFound
	}
	return e, nil
}

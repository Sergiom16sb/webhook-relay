// Package memory provides an in-memory adapter for application.EventStore.
// Useful for tests, dev mode, and small workloads where persistence
// doesn't matter. For production, replace with internal/infra/postgres.
package memory

import (
	"context"
	"sync"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// Compile-time check: this struct satisfies application.EventStore.
var _ application.EventStore = (*EventStore)(nil)

// EventStore is the in-memory implementation of the EventStore port.
// RWMutex: many concurrent reads OK, writes exclusive.
type EventStore struct {
	mu     sync.RWMutex
	byID   map[string]*domain.Event // for FindByID
	byHash map[string]*domain.Event // for idempotent dedup
}

// NewEventStore returns an initialized EventStore.
func NewEventStore() *EventStore {
	return &EventStore{
		byID:   make(map[string]*domain.Event),
		byHash: make(map[string]*domain.Event),
	}
}

// hashKey returns the dedup key for an event.
func hashKey(sourceID, payloadHash string) string {
	return sourceID + ":" + payloadHash
}

// Save stores the event idempotently by (source_id, payload_hash).
// Returns the event — whether newly inserted or pre-existing — so the
// caller always gets a non-nil event back (idempotent retry contract).
func (s *EventStore) Save(_ context.Context, e *domain.Event) (*domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := hashKey(e.SourceID, e.PayloadHash)
	if existing, ok := s.byHash[key]; ok {
		// Idempotent hit — return existing, don't insert.
		return existing, nil
	}

	s.byID[e.ID] = e
	s.byHash[key] = e
	return e, nil
}

// FindByID returns the event for the given ID.
func (s *EventStore) FindByID(_ context.Context, id string) (*domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[id]
	if !ok {
		return nil, application.ErrEventNotFound
	}
	return e, nil
}

// List returns a page of events ordered by insertion (newest first in v1).
// TODO Step 1.18: real ORDER BY created_at DESC + indexes.
func (s *EventStore) List(_ context.Context, limit, offset int) ([]*domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Inefficient but correct for v1: iterate map.
	all := make([]*domain.Event, 0, len(s.byID))
	for _, e := range s.byID {
		all = append(all, e)
	}
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

// Count returns total events in the store.
func (s *EventStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}

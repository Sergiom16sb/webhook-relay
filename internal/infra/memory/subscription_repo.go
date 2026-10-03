// Package memory provides an in-memory adapter for SubscriptionStore +
// DeliveryAttemptStore. Useful for tests.
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// Compile-time checks.
var (
	_ application.SubscriptionStore    = (*MemorySubscriptionStore)(nil)
	_ application.DeliveryAttemptStore = (*MemoryDeliveryAttemptStore)(nil)
)

// MemorySubscriptionStore implements SubscriptionStore in-memory.
type MemorySubscriptionStore struct {
	mu    sync.RWMutex
	byID  map[string]*domain.Subscription
	byKey map[string]*domain.Subscription // (source_id, target_url) → sub
}

func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{
		byID:  make(map[string]*domain.Subscription),
		byKey: make(map[string]*domain.Subscription),
	}
}

// (source_id, target_url) → sub key
func subKey(sourceID, targetURL string) string {
	return sourceID + "\x00" + targetURL
}

// Save upserts by (source_id, target_url). Returns persisted row.
func (s *MemorySubscriptionStore) Save(_ context.Context, sub *domain.Subscription) (*domain.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := subKey(sub.SourceID, sub.TargetURL)
	if existing, ok := s.byKey[key]; ok {
		// Update the existing in place to preserve ID.
		sub.ID = existing.ID
	}
	s.byID[sub.ID] = sub
	s.byKey[key] = sub
	return sub, nil
}

func (s *MemorySubscriptionStore) FindByID(_ context.Context, id string) (*domain.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.byID[id]
	if !ok {
		return nil, application.ErrEventNotFound
	}
	return sub, nil
}

func (s *MemorySubscriptionStore) ListBySourceID(_ context.Context, sourceID string) ([]*domain.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Subscription
	for _, sub := range s.byID {
		if sub.SourceID == sourceID && sub.Status == domain.SubscriptionStatusActive {
			out = append(out, sub)
		}
	}
	return out, nil
}

func (s *MemorySubscriptionStore) List(_ context.Context, limit, offset int) ([]*domain.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*domain.Subscription, 0, len(s.byID))
	for _, sub := range s.byID {
		all = append(all, sub)
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

func (s *MemorySubscriptionStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}

// --- MemoryDeliveryAttemptStore ---

// MemoryDeliveryAttemptStore implements DeliveryAttemptStore in-memory.
// Single-row state machine: one DeliveryAttempt per (event, subscription).
// Save is an upsert keyed on (event_id, subscription_id).
type MemoryDeliveryAttemptStore struct {
	mu sync.RWMutex
	// attempts[event_id][subscription_id] = attempt (one row per pair)
	attempts map[string]map[string]*domain.DeliveryAttempt
}

func NewMemoryDeliveryAttemptStore() *MemoryDeliveryAttemptStore {
	return &MemoryDeliveryAttemptStore{
		attempts: make(map[string]map[string]*domain.DeliveryAttempt),
	}
}

// Save upserts the attempt row for (event_id, subscription_id). Replaces
// any existing row (state transition: pending → success/failed/retrying).
func (s *MemoryDeliveryAttemptStore) Save(_ context.Context, a *domain.DeliveryAttempt) (*domain.DeliveryAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bySub, ok := s.attempts[a.EventID]
	if !ok {
		bySub = make(map[string]*domain.DeliveryAttempt)
		s.attempts[a.EventID] = bySub
	}
	bySub[a.SubscriptionID] = a
	return a, nil
}

func (s *MemoryDeliveryAttemptStore) ListByEventID(_ context.Context, eventID string) ([]*domain.DeliveryAttempt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.DeliveryAttempt
	for _, a := range s.attempts[eventID] {
		out = append(out, a)
	}
	// Order by started_at DESC so the admin UI matches the postgres
	// adapter's behavior. Map iteration order is randomized.
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out, nil
}

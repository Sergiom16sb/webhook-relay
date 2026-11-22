package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// fakeSubscriptionStore is a hand-rolled fake for subscription tests.
type fakeSubscriptionStore struct {
	byID  map[string]*domain.Subscription
	byKey map[string]*domain.Subscription
	err   error
}

func (f *fakeSubscriptionStore) Save(_ context.Context, s *domain.Subscription) (*domain.Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.byID[s.ID] = s
	f.byKey[s.SourceID+"\x00"+s.TargetURL] = s
	return s, nil
}

func (f *fakeSubscriptionStore) FindByID(_ context.Context, id string) (*domain.Subscription, error) {
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return nil, application.ErrSubscriptionNotFound
}

func (f *fakeSubscriptionStore) ListBySourceID(_ context.Context, _ string) ([]*domain.Subscription, error) {
	return nil, nil
}

func (f *fakeSubscriptionStore) List(_ context.Context, _, _ int) ([]*domain.Subscription, error) {
	var out []*domain.Subscription
	for _, s := range f.byID {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSubscriptionStore) Count(_ context.Context) (int, error) {
	return len(f.byID), nil
}

// Compile-time check.
var _ application.SubscriptionStore = (*fakeSubscriptionStore)(nil)

func TestCreateSubscription_Success(t *testing.T) {
	store := &fakeSubscriptionStore{byID: map[string]*domain.Subscription{}, byKey: map[string]*domain.Subscription{}}
	uc := application.NewCreateSubscriptionUseCase(store)

	got, err := uc.Create(context.Background(), "shopify", "https://example.com/webhook", "secret123")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == "" {
		t.Error("expected non-empty ID")
	}
	if got.Status != domain.SubscriptionStatusActive {
		t.Errorf("status = %q, want active", got.Status)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be populated")
	}
}

func TestCreateSubscription_ValidationError(t *testing.T) {
	store := &fakeSubscriptionStore{byID: map[string]*domain.Subscription{}, byKey: map[string]*domain.Subscription{}}
	uc := application.NewCreateSubscriptionUseCase(store)

	cases := []struct {
		name      string
		sourceID  string
		targetURL string
		secret    string
	}{
		{"empty source", "", "https://x.com/wh", "secret"},
		{"empty target", "src", "", "secret"},
		{"empty secret", "src", "https://x.com/wh", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uc.Create(context.Background(), tt.sourceID, tt.targetURL, tt.secret)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestDeleteSubscription_MarksDeleted(t *testing.T) {
	store := &fakeSubscriptionStore{byID: map[string]*domain.Subscription{}, byKey: map[string]*domain.Subscription{}}
	uc := application.NewDeleteSubscriptionUseCase(store)

	// Seed a subscription directly.
	store.byID["sub_1"] = &domain.Subscription{
		ID: "sub_1", SourceID: "src", TargetURL: "https://x.com/wh",
		Secret: "s", Status: domain.SubscriptionStatusActive, CreatedAt: time.Now(),
	}

	if err := uc.Delete(context.Background(), "sub_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.byID["sub_1"].Status != domain.SubscriptionStatusDeleted {
		t.Errorf("status = %q, want deleted", store.byID["sub_1"].Status)
	}
}

func TestDeleteSubscription_NotFound(t *testing.T) {
	store := &fakeSubscriptionStore{byID: map[string]*domain.Subscription{}, byKey: map[string]*domain.Subscription{}}
	uc := application.NewDeleteSubscriptionUseCase(store)

	err := uc.Delete(context.Background(), "missing")
	if !errors.Is(err, application.ErrSubscriptionNotFound) {
		t.Errorf("got %v, want ErrSubscriptionNotFound", err)
	}
}

// Avoid unused import error if time becomes unused.
var _ = time.Now

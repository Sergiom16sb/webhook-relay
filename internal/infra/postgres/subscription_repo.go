// Package postgres — Postgres-backed implementations of application ports.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// Compile-time check.
var _ application.SubscriptionStore = (*SubscriptionStore)(nil)

// SubscriptionStore is the Postgres-backed subscription repo.
type SubscriptionStore struct {
	db *sqlx.DB
}

func NewSubscriptionStore(db *sqlx.DB) *SubscriptionStore {
	return &SubscriptionStore{db: db}
}

type subscriptionRow struct {
	ID        string    `db:"id"`
	SourceID  string    `db:"source_id"`
	TargetURL string    `db:"target_url"`
	Secret    string    `db:"secret"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
}

func (r *subscriptionRow) toDomain() *domain.Subscription {
	return &domain.Subscription{
		ID:        r.ID,
		SourceID:  r.SourceID,
		TargetURL: r.TargetURL,
		Secret:    r.Secret,
		Status:    domain.SubscriptionStatus(r.Status),
		CreatedAt: r.CreatedAt,
	}
}

// Save persists a subscription with ON CONFLICT upsert on (source_id, target_url).
// Returns the persisted row (whether new or existing).
func (s *SubscriptionStore) Save(ctx context.Context, sub *domain.Subscription) (*domain.Subscription, error) {
	const q = `
		INSERT INTO subscriptions (id, source_id, target_url, secret, status, created_at)
		VALUES (:id, :source_id, :target_url, :secret, :status, :created_at)
		ON CONFLICT (source_id, target_url) DO UPDATE SET
			secret = EXCLUDED.secret,
			status = EXCLUDED.status
		RETURNING *
	`
	rows, err := s.db.NamedQueryContext(ctx, q, sub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.New("postgres: subscription upsert returned no rows")
	}
	var r subscriptionRow
	if err := rows.StructScan(&r); err != nil {
		return nil, err
	}
	return r.toDomain(), nil
}

// FindByID returns the subscription for the given ID.
func (s *SubscriptionStore) FindByID(ctx context.Context, id string) (*domain.Subscription, error) {
	const q = `SELECT id, source_id, target_url, secret, status, created_at FROM subscriptions WHERE id = $1`
	var r subscriptionRow
	if err := s.db.GetContext(ctx, &r, q, id); err != nil {
		return nil, err
	}
	return r.toDomain(), nil
}

// ListBySourceID returns active subscriptions for a given source.
// Used by the delivery use case to fan out events.
func (s *SubscriptionStore) ListBySourceID(ctx context.Context, sourceID string) ([]*domain.Subscription, error) {
	const q = `SELECT id, source_id, target_url, secret, status, created_at
	          FROM subscriptions WHERE source_id = $1 AND status = 'active'`
	rows, err := s.db.QueryxContext(ctx, q, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Subscription
	for rows.Next() {
		var r subscriptionRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, r.toDomain())
	}
	return out, rows.Err()
}

// List returns a page of subscriptions for admin UI.
func (s *SubscriptionStore) List(ctx context.Context, limit, offset int) ([]*domain.Subscription, error) {
	const q = `SELECT id, source_id, target_url, secret, status, created_at
	          FROM subscriptions ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := s.db.QueryxContext(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Subscription
	for rows.Next() {
		var r subscriptionRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, r.toDomain())
	}
	return out, rows.Err()
}

// Count returns total subscriptions.
func (s *SubscriptionStore) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.db.GetContext(ctx, &n, "SELECT COUNT(*) FROM subscriptions"); err != nil {
		return 0, err
	}
	return n, nil
}

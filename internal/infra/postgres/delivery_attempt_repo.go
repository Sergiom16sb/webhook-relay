// Package postgres — DeliveryAttemptStore adapter.
package postgres

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// Compile-time check.
var _ application.DeliveryAttemptStore = (*DeliveryAttemptStore)(nil)

// DeliveryAttemptStore records each attempt to deliver an event.
type DeliveryAttemptStore struct {
	db *sqlx.DB
}

func NewDeliveryAttemptStore(db *sqlx.DB) *DeliveryAttemptStore {
	return &DeliveryAttemptStore{db: db}
}

type deliveryAttemptRow struct {
	ID             string     `db:"id"`
	EventID        string     `db:"event_id"`
	SubscriptionID string     `db:"subscription_id"`
	AttemptNum     int        `db:"attempt_num"`
	AttemptCount   int        `db:"attempt_count"`
	Status         string     `db:"status"`
	StatusCode     int        `db:"status_code"`
	Error          string     `db:"error"`
	StartedAt      time.Time  `db:"started_at"`
	CompletedAt    *time.Time `db:"completed_at"`
}

func (r *deliveryAttemptRow) toDomain() *domain.DeliveryAttempt {
	return &domain.DeliveryAttempt{
		ID:             r.ID,
		EventID:        r.EventID,
		SubscriptionID: r.SubscriptionID,
		AttemptNum:     r.AttemptNum,
		AttemptCount:   r.AttemptCount,
		Status:         domain.DeliveryAttemptStatus(r.Status),
		StatusCode:     r.StatusCode,
		Error:          r.Error,
		StartedAt:      r.StartedAt,
		CompletedAt:    r.CompletedAt,
	}
}

// Save upserts the attempt row for (event_id, subscription_id). The
// state-machine design guarantees one row per pair — status transitions
// (pending → success | failed | retrying) overwrite the same row.
func (s *DeliveryAttemptStore) Save(ctx context.Context, a *domain.DeliveryAttempt) (*domain.DeliveryAttempt, error) {
	const q = `
		INSERT INTO delivery_attempts
		    (id, event_id, subscription_id, attempt_num, attempt_count, status, status_code, error, started_at, completed_at)
		VALUES
		    (:id, :event_id, :subscription_id, :attempt_num, :attempt_count, :status, :status_code, :error, :started_at, :completed_at)
		ON CONFLICT (event_id, subscription_id) DO UPDATE SET
		    attempt_num = EXCLUDED.attempt_num,
		    attempt_count = EXCLUDED.attempt_count,
		    status = EXCLUDED.status,
		    status_code = EXCLUDED.status_code,
		    error = EXCLUDED.error,
		    started_at = EXCLUDED.started_at,
		    completed_at = EXCLUDED.completed_at
		RETURNING *
	`
	rows, err := s.db.NamedQueryContext(ctx, q, a)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		// Should not happen with RETURNING — but be defensive.
		return nil, nil
	}
	var r deliveryAttemptRow
	if err := rows.StructScan(&r); err != nil {
		return nil, err
	}
	return r.toDomain(), nil
}

// ListByEventID returns all attempts for an event. With the state-machine
// design there's at most one row per (event, subscription), so the result
// has at most len(subscriptions) rows. Ordered by started_at DESC so the
// admin UI shows the most recent attempt first.
func (s *DeliveryAttemptStore) ListByEventID(ctx context.Context, eventID string) ([]*domain.DeliveryAttempt, error) {
	const q = `SELECT id, event_id, subscription_id, attempt_num, attempt_count, status, status_code, error, started_at, completed_at
	          FROM delivery_attempts WHERE event_id = $1
	          ORDER BY started_at DESC`
	rows, err := s.db.QueryxContext(ctx, q, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.DeliveryAttempt
	for rows.Next() {
		var r deliveryAttemptRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, r.toDomain())
	}
	return out, rows.Err()
}

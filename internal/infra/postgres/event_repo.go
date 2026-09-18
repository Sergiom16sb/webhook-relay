// Package postgres — EventStore adapter implementing application.EventStore.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// Compile-time check: this struct satisfies application.EventStore.
var _ application.EventStore = (*EventStore)(nil)

// EventStore is the Postgres-backed implementation of the EventStore port.
// All persistence goes through this struct; the rest of the app sees
// only the application.EventStore interface.
type EventStore struct {
	db *sqlx.DB
}

// NewEventStore constructs the adapter. Caller owns db (closes it).
func NewEventStore(db *sqlx.DB) *EventStore {
	return &EventStore{db: db}
}

// eventRow mirrors the DB schema for scanning. Different from domain.Event
// because Postgres types differ slightly (JSONB = []byte, TIMESTAMPTZ = time.Time).
type eventRow struct {
	ID        string    `db:"id"`
	SourceID  string    `db:"source_id"`
	Type      string    `db:"type"`
	Payload   []byte    `db:"payload"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
}

// toDomain converts a row to a domain.Event.
func (r *eventRow) toDomain() *domain.Event {
	return &domain.Event{
		ID:        r.ID,
		SourceID:  r.SourceID,
		Type:      r.Type,
		Payload:   r.Payload, // []byte satisfies json.RawMessage ([]byte alias)
		Status:    domain.EventStatus(r.Status),
		CreatedAt: r.CreatedAt,
	}
}

// Save persists an event. Upserts on ID conflict (caller controls idempotency).
func (s *EventStore) Save(ctx context.Context, e *domain.Event) error {
	const q = `
		INSERT INTO events (id, source_id, type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			source_id = EXCLUDED.source_id,
			type      = EXCLUDED.type,
			payload   = EXCLUDED.payload,
			status    = EXCLUDED.status
	`
	_, err := s.db.ExecContext(ctx, q,
		e.ID, e.SourceID, e.Type, []byte(e.Payload), string(e.Status), e.CreatedAt)
	return err
}

// FindByID returns the event for the given ID, or ErrEventNotFound.
func (s *EventStore) FindByID(ctx context.Context, id string) (*domain.Event, error) {
	const q = `SELECT id, source_id, type, payload, status, created_at FROM events WHERE id = $1`
	var row eventRow
	if err := s.db.GetContext(ctx, &row, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrEventNotFound
		}
		return nil, err
	}
	return row.toDomain(), nil
}

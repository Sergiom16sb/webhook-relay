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
	ID          string    `db:"id"`
	SourceID    string    `db:"source_id"`
	Type        string    `db:"type"`
	Payload     []byte    `db:"payload"`
	Status      string    `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
	PayloadHash string    `db:"payload_hash"`
}

// toDomain converts a row to a domain.Event.
func (r *eventRow) toDomain() *domain.Event {
	return &domain.Event{
		ID:          r.ID,
		SourceID:    r.SourceID,
		Type:        r.Type,
		Payload:     r.Payload, // []byte satisfies json.RawMessage ([]byte alias)
		Status:      domain.EventStatus(r.Status),
		CreatedAt:   r.CreatedAt,
		PayloadHash: r.PayloadHash,
	}
}

// Save persists an event idempotently by (source_id, payload_hash).
// Same tuple = existing event returned (no new row, no duplicate key).
//
// Implementation: INSERT ... ON CONFLICT DO UPDATE SET id = events.id RETURNING *
// - DO UPDATE with id = id is a no-op update that forces RETURNING to emit the row.
// - Whether we just inserted OR the row pre-existed, RETURNING gives us the same row.
// - The caller gets the same event ID either way (idempotent retry).
func (s *EventStore) Save(ctx context.Context, e *domain.Event) (*domain.Event, error) {
	const q = `
		INSERT INTO events (id, source_id, type, payload, status, created_at, payload_hash)
		VALUES (:id, :source_id, :type, :payload, :status, :created_at, :payload_hash)
		ON CONFLICT (source_id, payload_hash) DO UPDATE SET id = events.id
		RETURNING *
	`
	row := eventRow{
		ID:          e.ID,
		SourceID:    e.SourceID,
		Type:        e.Type,
		Payload:     []byte(e.Payload),
		Status:      string(e.Status),
		CreatedAt:   e.CreatedAt,
		PayloadHash: e.PayloadHash,
	}
	rows, err := s.db.NamedQueryContext(ctx, q, row)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		// RETURNING should always emit one row (either inserted or no-op-updated).
		// If we get here something is very wrong.
		return nil, errors.New("postgres: upsert returned no rows")
	}
	if err := rows.StructScan(&row); err != nil {
		return nil, err
	}
	return row.toDomain(), nil
}

// FindByID returns the event for the given ID, or ErrEventNotFound.
func (s *EventStore) FindByID(ctx context.Context, id string) (*domain.Event, error) {
	const q = `SELECT id, source_id, type, payload, status, created_at, payload_hash FROM events WHERE id = $1`
	var row eventRow
	if err := s.db.GetContext(ctx, &row, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrEventNotFound
		}
		return nil, err
	}
	return row.toDomain(), nil
}

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver registration
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
	"github.com/sgtour/webhook-relay/internal/infra/postgres"
)

// setup spins up a real Postgres via testcontainers, runs migrations,
// returns a connected EventStore + cleanup func.
func setup(t *testing.T) (*postgres.EventStore, func()) {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		tcpostgres.WithDatabase("webhook_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgC.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}

	db, err := sqlx.Open("pgx", connStr)
	if err != nil {
		_ = pgC.Terminate(ctx)
		t.Fatalf("open db: %v", err)
	}

	if err := postgres.Migrate(ctx, db); err != nil {
		_ = db.Close()
		_ = pgC.Terminate(ctx)
		t.Fatalf("migrate: %v", err)
	}

	store := postgres.NewEventStore(db)

	cleanup := func() {
		_ = db.Close()
		_ = pgC.Terminate(ctx)
	}
	return store, cleanup
}

func validEvent(id string) *domain.Event {
	return &domain.Event{
		ID:        id,
		SourceID:  "src_test",
		Type:      "test.created",
		Payload:   json.RawMessage(`{"order_id":"ord_42"}`),
		Status:    domain.EventStatusPending,
		CreatedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	}
}

func TestEventStore_SaveAndFindByID(t *testing.T) {
	store, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	in := validEvent("evt_1")
	if err := store.Save(ctx, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	out, err := store.FindByID(ctx, "evt_1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if out.ID != in.ID {
		t.Errorf("ID = %q, want %q", out.ID, in.ID)
	}
	if out.Status != domain.EventStatusPending {
		t.Errorf("status = %q, want pending", out.Status)
	}
	// Postgres JSONB normalizes whitespace when re-serializing
	// (`{"a":1}` becomes `{"a": 1}`). Compare semantically, not byte-by-byte.
	var inJSON, outJSON map[string]any
	if err := json.Unmarshal(in.Payload, &inJSON); err != nil {
		t.Fatalf("unmarshal in: %v", err)
	}
	if err := json.Unmarshal(out.Payload, &outJSON); err != nil {
		t.Fatalf("unmarshal out: %v", err)
	}
	if !reflect.DeepEqual(inJSON, outJSON) {
		t.Errorf("payload semantic mismatch:\n in:  %v\n out: %v", inJSON, outJSON)
	}
}

func TestEventStore_FindByID_NotFound(t *testing.T) {
	store, cleanup := setup(t)
	defer cleanup()

	_, err := store.FindByID(context.Background(), "missing")
	if !errors.Is(err, application.ErrEventNotFound) {
		t.Fatalf("got %v, want ErrEventNotFound", err)
	}
}

func TestEventStore_Overwrite(t *testing.T) {
	store, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()
	e1 := validEvent("evt_1")
	e1.Status = domain.EventStatusPending
	if err := store.Save(ctx, e1); err != nil {
		t.Fatal(err)
	}

	e2 := validEvent("evt_1")
	e2.Status = domain.EventStatusDelivered
	if err := store.Save(ctx, e2); err != nil {
		t.Fatal(err)
	}

	got, _ := store.FindByID(ctx, "evt_1")
	if got.Status != domain.EventStatusDelivered {
		t.Errorf("status = %q, want delivered (overwritten)", got.Status)
	}
}

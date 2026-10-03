// Tests for the store builders in main.go. The builders are tiny, but
// they are the single point of truth for "which backend does this
// deployment use?" — silent regressions here mean production runs
// against the wrong store, so we cover both branches.
package main

import (
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/sgtour/webhook-relay/internal/infra/memory"
	pgadapter "github.com/sgtour/webhook-relay/internal/infra/postgres"
)

// withSharedDB swaps sharedDB for the duration of the test. Restores on
// cleanup so tests stay order-independent.
func withSharedDB(t *testing.T, db *sqlx.DB) {
	t.Helper()
	orig := sharedDB
	sharedDB = db
	t.Cleanup(func() { sharedDB = orig })
}

func TestBuildAttemptStore_MemoryWhenSharedDBNil(t *testing.T) {
	withSharedDB(t, nil)

	store := buildAttemptStore()
	if _, ok := store.(*memory.MemoryDeliveryAttemptStore); !ok {
		t.Fatalf("got %T, want *memory.MemoryDeliveryAttemptStore", store)
	}
}

func TestBuildAttemptStore_PostgresWhenSharedDBSet(t *testing.T) {
	// DSN never connects — pgx is lazy and we never query. The
	// constructor only stores the pointer, so any valid DSN works.
	db, err := sqlx.Open("pgx", "postgres://localhost:5432/none?sslmode=disable")
	if err != nil {
		t.Fatalf("sqlx.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	withSharedDB(t, db)

	store := buildAttemptStore()
	if _, ok := store.(*pgadapter.DeliveryAttemptStore); !ok {
		t.Fatalf("got %T, want *pgadapter.DeliveryAttemptStore", store)
	}
}

func TestBuildSubStore_MemoryWhenSharedDBNil(t *testing.T) {
	// buildSubStore ignores its argument and reads sharedDB, so the
	// nil-interface test exercises the same code path the production
	// call uses.
	withSharedDB(t, nil)

	store := buildSubStore(nil)
	if _, ok := store.(*memory.MemorySubscriptionStore); !ok {
		t.Fatalf("got %T, want *memory.MemorySubscriptionStore", store)
	}
}

func TestBuildSubStore_PostgresWhenSharedDBSet(t *testing.T) {
	db, err := sqlx.Open("pgx", "postgres://localhost:5432/none?sslmode=disable")
	if err != nil {
		t.Fatalf("sqlx.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	withSharedDB(t, db)

	store := buildSubStore(nil)
	if _, ok := store.(*pgadapter.SubscriptionStore); !ok {
		t.Fatalf("got %T, want *pgadapter.SubscriptionStore", store)
	}
}

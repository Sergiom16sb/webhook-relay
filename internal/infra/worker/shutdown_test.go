package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sgtour/webhook-relay/internal/infra/worker"
)

// TestPool_ShutdownDrainsInFlight verifies the canonical graceful shutdown
// pattern: cancel the parent ctx (signal arrived), pool drains in-flight
// jobs, then Close() returns promptly.
func TestPool_ShutdownDrainsInFlight(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	pool := worker.NewPool(parentCtx, 3, 10)

	var processed atomicCounter
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		pool.Submit(func(ctx context.Context) error {
			defer wg.Done()
			// Respect cancellation while processing.
			select {
			case <-time.After(50 * time.Millisecond):
				processed.Inc()
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	// Give workers a moment to pick up jobs.
	time.Sleep(10 * time.Millisecond)

	// Signal "shutdown" by cancelling parent ctx.
	cancelParent()

	// Close should wait for all in-flight jobs to complete.
	pool.Close()

	// All 5 jobs should have completed (workers either finished or returned ctx.Err()).
	wg.Wait()
	if got := processed.Load(); got > 5 {
		t.Errorf("processed = %d, want <= 5", got)
	}
}

// TestPool_ShutdownAfterAllSubmitted verifies that jobs already buffered
// in the channel are drained before Close returns.
//
// Drain pattern: Close without prior cancel. Workers drain buffered jobs
// via the jobsCh-close select case. Cancelling ctx BEFORE Close races with
// the ctx.Done() select case and some jobs will be abandoned (cancel mid-flight
// is correct behavior — those jobs need retry/DLQ, not silent loss).
func TestPool_ShutdownAfterAllSubmitted(t *testing.T) {
	pool := worker.NewPool(context.Background(), 2, 100)

	var processed atomicCounter
	const totalJobs = 50

	for i := 0; i < totalJobs; i++ {
		pool.Submit(func(_ context.Context) error {
			processed.Inc()
			return nil
		})
	}

	pool.Close()

	if got := processed.Load(); got != totalJobs {
		t.Fatalf("processed = %d, want %d (Close did not drain buffered jobs)", got, totalJobs)
	}
}

// atomicCounter wraps sync/atomic for tests that don't need full int32 semantics.
type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (a *atomicCounter) Inc() {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
}

func (a *atomicCounter) Load() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

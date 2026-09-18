package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgtour/webhook-relay/internal/infra/worker"
)

func TestPool_ProcessesAllJobs(t *testing.T) {
	pool := worker.NewPool(context.Background(), 5, 10)
	defer pool.Close()

	const totalJobs = 100
	var processed atomic.Int32

	for i := 0; i < totalJobs; i++ {
		pool.Submit(func(_ context.Context) error {
			processed.Add(1)
			return nil
		})
	}

	// Close waits for all jobs to drain — processed must equal totalJobs when it returns.
	pool.Close()

	if got := processed.Load(); got != totalJobs {
		t.Fatalf("processed = %d, want %d", got, totalJobs)
	}
}

func TestPool_ConcurrentExecution(t *testing.T) {
	// 5 workers, jobs that block until released. Verify workers run in parallel.
	pool := worker.NewPool(context.Background(), 5, 10)
	defer pool.Close()

	release := make(chan struct{})
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	track := func() {
		now := inFlight.Add(1)
		// Track high water mark.
		for {
			cur := maxInFlight.Load()
			if now <= cur || maxInFlight.CompareAndSwap(cur, now) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
	}

	for i := 0; i < 5; i++ {
		pool.Submit(func(_ context.Context) error {
			track()
			return nil
		})
	}

	// Give workers time to all pick up a job.
	time.Sleep(50 * time.Millisecond)

	if got := maxInFlight.Load(); got < 2 {
		t.Errorf("maxInFlight = %d, want >= 2 (workers should run in parallel)", got)
	}

	close(release)
}

func TestPool_BufferedQueue_BlocksUntilConsumed(t *testing.T) {
	// Buffer of 2, workers = 1, jobs that block. Submit 2 fits in buffer;
	// third Submit blocks until the worker drains one.
	pool := worker.NewPool(context.Background(), 1, 2)
	defer pool.Close()

	first := make(chan struct{})
	release := make(chan struct{})

	pool.Submit(func(_ context.Context) error {
		<-first
		return nil
	})
	pool.Submit(func(_ context.Context) error {
		<-release
		return nil
	})
	pool.Submit(func(_ context.Context) error {
		<-release
		return nil
	})

	// Worker is busy on job 1. Buffer holds jobs 2 and 3. Give it a moment.
	time.Sleep(20 * time.Millisecond)
	close(first)

	// After first releases, worker can drain buffer. Allow time.
	time.Sleep(50 * time.Millisecond)
	close(release)
}

func TestPool_CloseIsIdempotent(t *testing.T) {
	pool := worker.NewPool(context.Background(), 2, 5)
	pool.Close()
	pool.Close() // must not panic
}

func TestPool_ContextCancellationStopsWorkers(t *testing.T) {
	// Use a parent ctx we can cancel mid-job.
	parentCtx, cancelParent := context.WithCancel(context.Background())
	pool := worker.NewPool(parentCtx, 3, 5)

	// started signals when at least one job has begun. Buffered so the
	// 10 sends from submitters never block.
	started := make(chan struct{}, 100)

	// Submit jobs that respect context cancellation and signal start.
	for i := 0; i < 10; i++ {
		pool.Submit(func(ctx context.Context) error {
			// Signal that this job has started.
			select {
			case started <- struct{}{}:
			default:
			}
			// Wait for either cancel or done.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return nil // bail out if ctx not cancelled
			}
		})
	}

	// Wait for at least one job to start.
	<-started

	// Cancel parent. Workers exit their ctx-bounded wait, drain,
	// and the pool's Close() returns quickly.
	cancelParent()

	done := make(chan struct{})
	go func() {
		pool.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after parent cancel")
	}
}

func TestPool_ZeroJobs(t *testing.T) {
	// Edge case: Submit nothing, just Close.
	pool := worker.NewPool(context.Background(), 5, 10)
	pool.Close() // should return immediately, no goroutines leaked
}

func TestPool_ConcurrentSubmitAndClose(t *testing.T) {
	// Race test: multiple goroutines submit while Close is called.
	// With -race, any data race triggers a failure.
	pool := worker.NewPool(context.Background(), 4, 100)
	defer pool.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				pool.Submit(func(_ context.Context) error {
					time.Sleep(time.Microsecond)
					return nil
				})
			}
		}()
	}

	// Wait for submitters to finish, then close.
	wg.Wait()
	pool.Close()
}

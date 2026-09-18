// Package worker provides a bounded worker pool with cancellation.
//
// Topology (from go-concurrency skill):
//
//	[Submitter] ──jobsCh──> [worker_1..N] ──> job(ctx) result
//	                  │
//	              (closed by Close)
//
// Discipline:
//   - Submitter (caller) NEVER closes the channel. Pool.Close() does.
//   - wg.Add(1) runs BEFORE go func() — never inside.
//   - Workers exit on ctx.Done() OR jobsCh close.
//   - Pool.Close() waits for workers before cancelling (drain pattern).
package worker

import (
	"context"
	"sync"
)

// Job is a unit of work. The function MUST respect ctx cancellation:
// return early when ctx.Done() is signaled.
type Job func(ctx context.Context) error

// Pool is a fixed-size worker pool. Submit jobs, then Close to drain.
type Pool struct {
	workers int
	jobs    chan Job
	wg      sync.WaitGroup

	ctx     context.Context
	cancel  context.CancelFunc
	closeMu sync.Mutex // protects Close from double-close panic
	closed  bool
}

// NewPool creates a pool with N workers and a buffered jobs channel.
// workers MUST be > 0. bufferSize is the backpressure boundary — when full,
// Submit blocks until a worker picks up a job.
//
// The parent ctx is the long-lived context the pool runs under. The pool
// derives its own cancellable context; Close() cancels it after drain.
func NewPool(parent context.Context, workers, bufferSize int) *Pool {
	if workers <= 0 {
		panic("worker.NewPool: workers must be > 0")
	}
	if bufferSize < 0 {
		panic("worker.NewPool: bufferSize must be >= 0")
	}

	ctx, cancel := context.WithCancel(parent)
	p := &Pool{
		workers: workers,
		jobs:    make(chan Job, bufferSize),
		ctx:     ctx,
		cancel:  cancel,
	}

	for i := 0; i < workers; i++ {
		p.wg.Add(1) // BEFORE go func, per skill rule
		go p.run()
	}
	return p
}

// Submit enqueues a job. Blocks if the buffer is full (backpressure).
// Panics if called after Close — Submit-after-Close is a programmer error.
func (p *Pool) Submit(j Job) {
	p.closeMu.Lock()
	closed := p.closed
	p.closeMu.Unlock()
	if closed {
		panic("worker.Pool: Submit after Close")
	}
	p.jobs <- j
}

// Close stops accepting new jobs and waits for all in-flight to finish.
// Idempotent — safe to call from defer and from a shutdown signal.
// After Close returns, no workers are running.
func (p *Pool) Close() {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return
	}
	p.closed = true
	close(p.jobs) // sender-side close (we are the only sender)
	p.closeMu.Unlock()

	p.wg.Wait() // wait for workers to drain
	p.cancel()  // then cancel any straggler contexts
}

// run is the worker loop. Exits on ctx cancel or jobsCh close.
func (p *Pool) run() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return // channel closed = no more work
			}
			// Errors ignored here — Step 1.12 adds retry/DLQ.
			// The pool contract: drain everything, don't bail mid-pool.
			_ = job(p.ctx)
		}
	}
}

// Context exposes the pool's cancellable context. Use it to derive
// child contexts for jobs (e.g., for per-job timeouts).
func (p *Pool) Context() context.Context {
	return p.ctx
}

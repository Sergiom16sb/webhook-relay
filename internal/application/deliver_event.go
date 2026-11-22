package application

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// DeliverySentinels — classify delivery failures.
var (
	// ErrTransient indicates a failure that may succeed on retry
	// (network timeout, target 5xx, rate limit).
	ErrTransient = errors.New("delivery: transient failure")

	// ErrPermanent indicates a failure that retry won't fix
	// (target 4xx except 408/429, malformed payload).
	ErrPermanent = errors.New("delivery: permanent failure")

	// ErrMaxAttemptsExceeded — all retries exhausted; DLQ candidate.
	ErrMaxAttemptsExceeded = errors.New("delivery: max attempts exceeded")
)

// RetryPolicy configures exponential backoff with jitter.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first
	BaseDelay   time.Duration // delay before attempt 2 = BaseDelay ± jitter
	MaxDelay    time.Duration // cap on per-attempt delay
}

// DefaultRetryPolicy — 7 attempts, 1s base, 60s cap.
// Total time: ~123s = ~2 min before DLQ.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 7,
		BaseDelay:   1 * time.Second,
		MaxDelay:    60 * time.Second,
	}
}

// backoff returns the delay before the Nth attempt (1-indexed).
// Formula: base * 2^(attempt-1), capped at maxDelay, with equal jitter.
//
//	attempt 1: no delay (first try)
//	attempt 2: BaseDelay + jitter
//	attempt 3: 2*BaseDelay + jitter
//	...
//
// Equal jitter: delay = baseDelay + random(0, baseDelay).
// Why: spreads the thundering herd when many deliveries fail at once.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	baseDelay := p.BaseDelay << (attempt - 2) // base * 2^(attempt-2)
	if baseDelay > p.MaxDelay || baseDelay < 0 {
		baseDelay = p.MaxDelay
	}
	jitter := time.Duration(rand.Int64N(int64(baseDelay)))
	return baseDelay + jitter
}

// BackoffForTest exposes the backoff calculation for testing.
// Production code uses backoff() via the use case.
func (p RetryPolicy) BackoffForTest(attempt int) time.Duration {
	return p.backoff(attempt)
}

// EventPoster delivers a payload to a URL. Returns classification of the
// outcome: nil = success, ErrTransient = retry, ErrPermanent = DLQ, or
// a wrapped error for other failure modes.
type EventPoster interface {
	Post(ctx context.Context, targetURL string, event *domain.Event) error
}

// DeliverEventUseCase delivers an event to a subscription URL with retry.
//
// Retry policy: exponential backoff with equal jitter, capped at MaxDelay.
// After MaxAttempts transient failures, returns ErrMaxAttemptsExceeded
// (caller should route to DLQ). On permanent failure, returns immediately
// (no retry). On success, returns nil.
//
// This use case is **synchronous** (v1) — the HTTP handler waits for delivery
// before returning. v2 will move delivery to the worker pool + async.
type DeliverEventUseCase struct {
	poster EventPoster
	policy RetryPolicy
}

// NewDeliverEventUseCase constructs the use case with the injected poster.
func NewDeliverEventUseCase(poster EventPoster, policy RetryPolicy) *DeliverEventUseCase {
	return &DeliverEventUseCase{poster: poster, policy: policy}
}

// Deliver attempts to POST the event to targetURL up to MaxAttempts times.
// Returns nil on success, ErrPermanent on first non-retryable failure,
// ErrMaxAttemptsExceeded after exhausting retries, or ctx.Err() if the
// context is cancelled mid-backoff.
func (uc *DeliverEventUseCase) Deliver(ctx context.Context, targetURL string, e *domain.Event) error {
	for attempt := 1; attempt <= uc.policy.MaxAttempts; attempt++ {
		// Cancel-safe sleep BEFORE the attempt: if ctx is done, bail early.
		if attempt > 1 {
			wait := uc.policy.backoff(attempt)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		err := uc.poster.Post(ctx, targetURL, e)
		if err == nil {
			return nil // success
		}

		// Classify: permanent → bail immediately (no retry).
		if errors.Is(err, ErrPermanent) {
			return fmt.Errorf("permanent at attempt %d: %w", attempt, err)
		}

		// Transient or unknown: continue retry loop. (Unknown errors are
		// treated as transient for safety — better to retry than to lose.)
	}

	// All attempts exhausted.
	return fmt.Errorf("after %d attempts: %w", uc.policy.MaxAttempts, ErrMaxAttemptsExceeded)
}

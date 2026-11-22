package application_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
	infrahttp "github.com/Sergiom16sb/webhook-relay/internal/infra/http"
)

// fakePoster records call count and returns scripted responses.
type fakePoster struct {
	calls   atomic.Int32
	results []error // cycle through on each call
}

func (f *fakePoster) Post(_ context.Context, _ string, _ *domain.Event) error {
	idx := int(f.calls.Add(1)) - 1
	if idx < len(f.results) {
		return f.results[idx]
	}
	// Out of scripted responses — return last (or nil if empty)
	if len(f.results) == 0 {
		return nil
	}
	return f.results[len(f.results)-1]
}

func validEvent() *domain.Event {
	return &domain.Event{
		ID:        "evt_1",
		SourceID:  "src",
		Type:      "test.created",
		Payload:   []byte(`{"ok":true}`),
		Status:    domain.EventStatusPending,
		CreatedAt: time.Now(),
	}
}

// TestUseCase_SuccessNoRetry — single call, returns nil, no retries.
func TestDeliverEvent_SuccessNoRetry(t *testing.T) {
	fake := &fakePoster{results: []error{nil}}
	uc := application.NewDeliverEventUseCase(fake, application.RetryPolicy{
		MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second,
	})

	if err := uc.Deliver(context.Background(), "http://x", validEvent()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := fake.calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// TestDeliverEvent_TransientThenSuccess — retry succeeds.
func TestDeliverEvent_TransientThenSuccess(t *testing.T) {
	transient := application.ErrTransient
	fake := &fakePoster{results: []error{
		fmt.Errorf("attempt 1: %w", transient),
		fmt.Errorf("attempt 2: %w", transient),
		nil, // success on third try
	}}
	uc := application.NewDeliverEventUseCase(fake, application.RetryPolicy{
		MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second,
	})

	if err := uc.Deliver(context.Background(), "http://x", validEvent()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := fake.calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (2 transient + 1 success)", got)
	}
}

// TestDeliverEvent_AlwaysTransientGoesToDLQ — all transient → ErrMaxAttemptsExceeded.
func TestDeliverEvent_AlwaysTransientGoesToDLQ(t *testing.T) {
	transient := application.ErrTransient
	results := make([]error, 5)
	for i := range results {
		results[i] = fmt.Errorf("attempt %d: %w", i+1, transient)
	}
	fake := &fakePoster{results: results}
	uc := application.NewDeliverEventUseCase(fake, application.RetryPolicy{
		MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second,
	})

	err := uc.Deliver(context.Background(), "http://x", validEvent())
	if err == nil {
		t.Fatal("expected error after max attempts, got nil")
	}
	if !errors.Is(err, application.ErrMaxAttemptsExceeded) {
		t.Errorf("got %v, want ErrMaxAttemptsExceeded", err)
	}
	if got := fake.calls.Load(); got != 5 {
		t.Errorf("calls = %d, want 5", got)
	}
}

// TestDeliverEvent_PermanentNoRetry — 4xx returns immediately.
func TestDeliverEvent_PermanentNoRetry(t *testing.T) {
	fake := &fakePoster{results: []error{
		fmt.Errorf("attempt 1: %w", application.ErrPermanent),
		nil, // never reached
	}}
	uc := application.NewDeliverEventUseCase(fake, application.RetryPolicy{
		MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second,
	})

	err := uc.Deliver(context.Background(), "http://x", validEvent())
	if err == nil {
		t.Fatal("expected permanent error, got nil")
	}
	if !errors.Is(err, application.ErrPermanent) {
		t.Errorf("got %v, want ErrPermanent", err)
	}
	if got := fake.calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on permanent)", got)
	}
}

// TestDeliverEvent_ContextCancelDuringBackoff — exit promptly.
func TestDeliverEvent_ContextCancelDuringBackoff(t *testing.T) {
	transient := application.ErrTransient
	fake := &fakePoster{results: []error{
		fmt.Errorf("attempt 1: %w", transient),
		fmt.Errorf("attempt 2: %w", transient),
		nil,
	}}
	// Use a long BaseDelay so the backoff is interruptible.
	uc := application.NewDeliverEventUseCase(fake, application.RetryPolicy{
		MaxAttempts: 5, BaseDelay: 10 * time.Second, MaxDelay: time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first failure is recorded, during the backoff wait.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := uc.Deliver(ctx, "http://x", validEvent())
	elapsed := time.Since(start)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Deliver took %v with cancellation; should exit promptly", elapsed)
	}
}

// TestRetryPolicy_BackoffWithinBounds — delay is base * 2^(attempt-2) ± jitter,
// capped at MaxDelay. We test the backoff function directly (cheaper,
// deterministic bounds).
func TestRetryPolicy_BackoffWithinBounds(t *testing.T) {
	const base = 100 * time.Millisecond
	policy := application.RetryPolicy{
		MaxAttempts: 7, BaseDelay: base, MaxDelay: 5 * base,
	}

	// attempt 1: no delay (first try).
	if got := policy.BackoffForTest(1); got != 0 {
		t.Errorf("attempt 1: backoff = %v, want 0", got)
	}

	// attempt N: in range [baseUsed, baseUsed * 2) where baseUsed =
	// min(base * 2^(N-2), MaxDelay). Equal jitter = baseUsed + random(0, baseUsed).
	for attempt := 2; attempt <= 6; attempt++ {
		rawBase := base << (attempt - 2)
		baseUsed := rawBase
		if baseUsed > 5*base {
			baseUsed = 5 * base
		}
		upperBound := baseUsed + baseUsed // = baseUsed * 2

		for i := 0; i < 100; i++ {
			got := policy.BackoffForTest(attempt)
			if got < baseUsed || got >= upperBound {
				t.Errorf("attempt %d sample %d: backoff %v outside [%v, %v)",
					attempt, i, got, baseUsed, upperBound)
			}
		}
	}
}

// TestPoster_RetryClassifier — verify the poster returns the right sentinel
// for each HTTP status code, using a real httptest server.
func TestPoster_RetryClassifier(t *testing.T) {
	cases := []struct {
		status        int
		wantTransient bool
	}{
		{200, false}, // success
		{201, false}, // created
		{204, false}, // no content
		{301, false}, // redirect (client follows automatically)
		{400, false}, // permanent (bad request, won't fix on retry)
		{401, false}, // permanent (auth)
		{403, false}, // permanent (forbidden)
		{404, false}, // permanent (not found)
		{408, true},  // transient (request timeout)
		{429, true},  // transient (rate limit)
		{500, true},  // transient (server error)
		{502, true},  // transient (bad gateway)
		{503, true},  // transient (service unavailable)
		{504, true},  // transient (gateway timeout)
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			p := infrahttp.NewPoster(time.Second)
			err := p.Post(context.Background(), srv.URL, validEvent())

			if tc.status >= 200 && tc.status < 400 {
				if err != nil {
					t.Errorf("status %d: want nil, got %v", tc.status, err)
				}
				return
			}

			if err == nil {
				t.Fatalf("status %d: want error, got nil", tc.status)
			}
			if tc.wantTransient {
				if !errors.Is(err, application.ErrTransient) {
					t.Errorf("status %d: got %v, want ErrTransient", tc.status, err)
				}
			} else {
				if !errors.Is(err, application.ErrPermanent) {
					t.Errorf("status %d: got %v, want ErrPermanent", tc.status, err)
				}
			}
		})
	}
}

// silence unused imports if test shrinks
var _ = errors.New

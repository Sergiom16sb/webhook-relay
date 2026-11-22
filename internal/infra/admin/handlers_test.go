// Tests for admin handlers, focused on the data-loading path that
// fetches delivery attempts + resolves their target URLs from
// subscriptions. Templ rendering is exercised end-to-end — we assert on
// substrings of the rendered HTML, which keeps the tests honest about
// the actual user-visible output.
package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/domain"
	"github.com/Sergiom16sb/webhook-relay/internal/infra/memory"
)

func ptrTime(t time.Time) *time.Time { return &t }

// seedEventWithAttempts is intentionally unused — kept commented in
// the file as a reference for the seeding pattern. Use seedInto for
// the actual tests.

func TestEventDetail_LoadsAttemptsWithTargetURL(t *testing.T) {
	h := NewHandlers(
		memory.NewMemoryEventStore(),
		memory.NewMemorySubscriptionStore(),
		memory.NewMemoryDeliveryAttemptStore(),
		nil, nil, nil, "",
	)
	const (
		urlA = "https://a.example.com/hook"
		urlB = "https://b.example.com/hook"
	)
	seedInto(t, h, "evt_1", urlA, urlB)

	req := httptest.NewRequest("GET", "/admin/events/evt_1", nil)
	rec := httptest.NewRecorder()
	h.EventDetail(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{urlA, urlB, "200", "500", "boom"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered HTML missing %q; snippet = %s", want, snippet(body, want))
		}
	}
}

func TestEventDetail_NoAttempts(t *testing.T) {
	eventStore := memory.NewMemoryEventStore()
	subStore := memory.NewMemorySubscriptionStore()
	attemptStore := memory.NewMemoryDeliveryAttemptStore()

	ev := &domain.Event{ID: "evt_2", SourceID: "src", Type: "x", Status: domain.EventStatusPending}
	if _, err := eventStore.Save(context.Background(), ev); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := NewHandlers(eventStore, subStore, attemptStore, nil, nil, nil, "")

	req := httptest.NewRequest("GET", "/admin/events/evt_2", nil)
	rec := httptest.NewRecorder()
	h.EventDetail(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No attempts yet") {
		t.Errorf("missing 'No attempts yet' message in body = %s", rec.Body.String())
	}
}

func TestEventDetail_NotFound(t *testing.T) {
	h := NewHandlers(memory.NewMemoryEventStore(), memory.NewMemorySubscriptionStore(), memory.NewMemoryDeliveryAttemptStore(), nil, nil, nil, "")

	req := httptest.NewRequest("GET", "/admin/events/missing", nil)
	rec := httptest.NewRecorder()
	h.EventDetail(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// seedInto populates the handler's own stores with one event, two
// subscriptions, and two delivery attempts (one per subscription).
// Tests pass urlA and urlB so the assertions can reference the exact
// target strings the rendered HTML should contain.
func seedInto(t *testing.T, h *Handlers, eventID, urlA, urlB string) {
	t.Helper()
	ev := &domain.Event{
		ID: eventID, SourceID: "src", Type: "order.created",
		Status: domain.EventStatusPending, CreatedAt: time.Now(),
	}
	if _, err := h.store.Save(context.Background(), ev); err != nil {
		t.Fatalf("save event: %v", err)
	}
	subA, err := h.subscriptionStore.Save(context.Background(), &domain.Subscription{
		ID: "sub_a", SourceID: "src", TargetURL: urlA, Secret: "s", Status: domain.SubscriptionStatusActive,
	})
	if err != nil {
		t.Fatalf("save subA: %v", err)
	}
	subB, err := h.subscriptionStore.Save(context.Background(), &domain.Subscription{
		ID: "sub_b", SourceID: "src", TargetURL: urlB, Secret: "s", Status: domain.SubscriptionStatusActive,
	})
	if err != nil {
		t.Fatalf("save subB: %v", err)
	}
	now := time.Now()
	if _, err := h.attemptStore.Save(context.Background(), &domain.DeliveryAttempt{
		ID: "a1", EventID: ev.ID, SubscriptionID: subA.ID,
		AttemptNum: 1, AttemptCount: 1,
		Status:      domain.DeliveryAttemptSuccess,
		StatusCode:  200,
		StartedAt:   now.Add(-2 * time.Second),
		CompletedAt: ptrTime(now.Add(-1500 * time.Millisecond)),
	}); err != nil {
		t.Fatalf("save attemptA: %v", err)
	}
	if _, err := h.attemptStore.Save(context.Background(), &domain.DeliveryAttempt{
		ID: "a2", EventID: ev.ID, SubscriptionID: subB.ID,
		AttemptNum: 1, AttemptCount: 1,
		Status:      domain.DeliveryAttemptFailed,
		StatusCode:  500,
		StartedAt:   now.Add(-1 * time.Second),
		CompletedAt: ptrTime(now),
		Error:       "boom",
	}); err != nil {
		t.Fatalf("save attemptB: %v", err)
	}
}

// snippet returns a small context window around want in body for
// readable failure output. Returns the full body if want is not found.
func snippet(body, want string) string {
	i := strings.Index(body, want)
	if i < 0 {
		return body
	}
	lo := i - 40
	if lo < 0 {
		lo = 0
	}
	hi := i + len(want) + 40
	if hi > len(body) {
		hi = len(body)
	}
	return body[lo:hi]
}

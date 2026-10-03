// Package admin — HTTP handlers for the /admin/* routes.
// Renders templ components; all logic lives in use cases / ports.
package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// Handlers wires the admin routes to templ components.
type Handlers struct {
	store             application.EventStore
	subscriptionStore application.SubscriptionStore
	attemptStore      application.DeliveryAttemptStore
	createSub         *application.CreateSubscriptionUseCase
	deleteSub         *application.DeleteSubscriptionUseCase
	verifier          application.SignatureVerifier // for HMAC helper default secret
	secret            string                        // default secret for HMAC form
}

// NewHandlers constructs the admin handlers.
func NewHandlers(
	store application.EventStore,
	subscriptionStore application.SubscriptionStore,
	attemptStore application.DeliveryAttemptStore,
	createSub *application.CreateSubscriptionUseCase,
	deleteSub *application.DeleteSubscriptionUseCase,
	verifier application.SignatureVerifier,
	secret string,
) *Handlers {
	return &Handlers{
		store:             store,
		subscriptionStore: subscriptionStore,
		attemptStore:      attemptStore,
		createSub:         createSub,
		deleteSub:         deleteSub,
		verifier:          verifier,
		secret:            secret,
	}
}

// Dashboard handles GET /admin.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	total, successRate, pending, failed, recent := h.kpis(ctx)

	props := DashboardProps{
		TotalLast24h: total,
		SuccessRate:  successRate,
		Pending:      pending,
		Failed:       failed,
		Recent:       recent,
	}
	render(w, r, Dashboard(props))
}

// EventsList handles GET /admin/events?status=&source=&q=&page=
func (h *Handlers) EventsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	filter := FilterState{
		Status: q.Get("status"),
		Source: q.Get("source"),
		Q:      q.Get("q"),
	}
	page := 1
	if p := q.Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	const pageSize = 25
	offset := (page - 1) * pageSize

	rows, err := h.queryEvents(ctx, filter, pageSize, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	total, _ := h.countEvents(ctx, filter)

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	props := EventsListProps{
		Events:     rows,
		Filter:     filter,
		Page:       page,
		TotalPages: totalPages,
		Total:      total,
	}
	render(w, r, EventsList(props))
}

// EventDetail handles GET /admin/events/{id}
func (h *Handlers) EventDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := extractEventID(r.URL.Path, "/admin/events/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	event, err := h.store.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrEventNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	attempts, err := h.attemptStore.ListByEventID(ctx, event.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Pre-load all subscriptions once to build a TargetURL lookup table.
	// With small N (typically <20 subs) this is cheaper than per-attempt
	// FindByID calls and naturally handles deleted subs.
	subs, _ := h.subscriptionStore.List(ctx, 1000, 0)
	subByID := make(map[string]*domain.Subscription, len(subs))
	for _, s := range subs {
		subByID[s.ID] = s
	}

	rows := make([]AttemptRow, 0, len(attempts))
	for _, a := range attempts {
		var target string
		if sub, ok := subByID[a.SubscriptionID]; ok {
			target = sub.TargetURL
		}
		rows = append(rows, AttemptRow{
			At:         a.StartedAt,
			TargetURL:  target,
			StatusCode: a.StatusCode,
			Duration:   fmtDuration(a.StartedAt, a.CompletedAt),
			Error:      a.Error,
		})
	}

	props := EventDetailProps{
		Event:    event,
		Attempts: rows,
	}
	render(w, r, EventDetail(props))
}

// RetryEvent handles POST /admin/events/{id}/retry
// Resets status to pending so the worker picks it up again.
func (h *Handlers) RetryEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := extractEventID(r.URL.Path, "/admin/events/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	event, err := h.store.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrEventNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset status to pending so the worker pool picks it up.
	event.Status = domain.EventStatusPending
	saved, err := h.store.Save(ctx, event)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = saved

	w.WriteHeader(http.StatusOK)
	// hx-on::after-request="location.reload()" in template handles UI refresh.
}

// HMACForm handles GET /admin/hmac — empty form.
func (h *Handlers) HMACForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, HMACHelper(HMACHelperProps{
		Secret:    h.secret,
		Payload:   `{"id":"e1","source_id":"src","type":"t","payload":{}}`,
		Signature: "",
	}))
}

// HMACCompute handles POST /admin/hmac — computes signature, re-renders with result.
func (h *Handlers) HMACCompute(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	secret := r.FormValue("secret")
	payload := r.FormValue("payload")

	sig := ""
	if secret != "" && payload != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		sig = hex.EncodeToString(mac.Sum(nil))
	}

	render(w, r, HMACHelper(HMACHelperProps{
		Secret:    secret,
		Payload:   payload,
		Signature: sig,
	}))
}

// --- Helpers ---

// render writes a templ component to the response. 500 on render error.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		// Headers already written — best we can do is log.
		fmt.Printf("admin: render error: %v\n", err)
	}
}

// extractEventID parses the id from /admin/events/{id}[/...] URLs.
func extractEventID(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	// rest is "id" or "id/retry" etc.
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// queryEvents fetches a page of events matching the filter.
func (h *Handlers) queryEvents(ctx context.Context, f FilterState, limit, offset int) ([]EventRow, error) {
	events, err := h.store.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	filtered := events
	if f.Status != "" {
		filtered = filterByStatus(filtered, f.Status)
	}
	if f.Source != "" {
		filtered = filterBySource(filtered, f.Source)
	}
	if f.Q != "" {
		filtered = filterByIDContains(filtered, f.Q)
	}
	return toRows(filtered), nil
}

// countEvents returns the total matching the filter (no pagination).
func (h *Handlers) countEvents(ctx context.Context, f FilterState) (int, error) {
	events, err := h.store.List(ctx, 1_000_000, 0) // v1: large limit to approximate count
	if err != nil {
		return 0, err
	}
	filtered := events
	if f.Status != "" {
		filtered = filterByStatus(filtered, f.Status)
	}
	if f.Source != "" {
		filtered = filterBySource(filtered, f.Source)
	}
	if f.Q != "" {
		filtered = filterByIDContains(filtered, f.Q)
	}
	return len(filtered), nil
}

// kpis computes the dashboard stats from recent events.
func (h *Handlers) kpis(ctx context.Context) (total int, successRate float64, pending, failed int, recent []EventRow) {
	all, err := h.store.List(ctx, 1000, 0)
	if err != nil {
		return 0, 0, 0, 0, nil
	}
	total = len(all)
	for _, e := range all {
		switch e.Status {
		case domain.EventStatusPending:
			pending++
		case domain.EventStatusFailed:
			failed++
		}
	}
	delivered := total - pending - failed
	if total > 0 {
		successRate = float64(delivered) / float64(total) * 100
	}

	// Recent = up to 10 most recent (already ordered DESC by List).
	if len(all) > 10 {
		all = all[:10]
	}
	recent = toRows(all)
	return
}

// --- Pure filter helpers (could move to a sub-package) ---

func filterByStatus(es []*domain.Event, status string) []*domain.Event {
	out := make([]*domain.Event, 0, len(es))
	for _, e := range es {
		if string(e.Status) == status {
			out = append(out, e)
		}
	}
	return out
}

func filterBySource(es []*domain.Event, source string) []*domain.Event {
	out := make([]*domain.Event, 0, len(es))
	for _, e := range es {
		if e.SourceID == source {
			out = append(out, e)
		}
	}
	return out
}

func filterByIDContains(es []*domain.Event, q string) []*domain.Event {
	out := make([]*domain.Event, 0, len(es))
	for _, e := range es {
		if strings.Contains(e.ID, q) {
			out = append(out, e)
		}
	}
	return out
}

func toRows(es []*domain.Event) []EventRow {
	out := make([]EventRow, len(es))
	for i, e := range es {
		out[i] = EventRow{
			ID:        e.ID,
			SourceID:  e.SourceID,
			Type:      e.Type,
			Status:    string(e.Status),
			CreatedAt: e.CreatedAt,
		}
	}
	return out
}

// silence unused imports if shrinking
var _ = time.Second

// --- Subscription handlers (v2) ---

// SubscriptionsList handles GET /admin/subscriptions.
// Lists all subscriptions + shows the create form.
func (h *Handlers) SubscriptionsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const pageSize = 50
	rows, err := h.subscriptionStore.List(ctx, pageSize, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	total, _ := h.subscriptionStore.Count(ctx)

	props := SubscriptionsListProps{
		Rows:   subRowsFromDomain(rows),
		Total:  total,
		Secret: h.secret,
	}
	render(w, r, SubscriptionsList(props))
}

// SubscriptionCreate handles POST /admin/subscriptions.
// Parses form, calls CreateSubscription use case, redirects to list.
func (h *Handlers) SubscriptionCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sourceID := strings.TrimSpace(r.FormValue("source_id"))
	targetURL := strings.TrimSpace(r.FormValue("target_url"))
	secret := strings.TrimSpace(r.FormValue("secret"))

	if sourceID == "" || targetURL == "" || secret == "" {
		http.Error(w, "source_id, target_url, and secret are required", http.StatusBadRequest)
		return
	}

	sub, err := h.createSub.Create(r.Context(), sourceID, targetURL, secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Redirect back to list with the new sub highlighted.
	http.Redirect(w, r, "/admin/subscriptions?highlight="+sub.ID, http.StatusSeeOther)
}

// SubscriptionDelete handles POST /admin/subscriptions/{id}/delete.
func (h *Handlers) SubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	id := extractSubscriptionID(r.URL.Path, "/admin/subscriptions/", "/delete")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if err := h.deleteSub.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/subscriptions", http.StatusSeeOther)
}

// extractSubscriptionID parses the id from /admin/subscriptions/{id}[/delete].
func extractSubscriptionID(path, prefix, suffix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.Index(rest, suffix); i >= 0 {
		return rest[:i]
	}
	return rest
}

// subRowsFromDomain converts domain subscriptions to admin template rows.
func subRowsFromDomain(subs []*domain.Subscription) []SubscriptionRow {
	out := make([]SubscriptionRow, len(subs))
	for i, s := range subs {
		out[i] = SubscriptionRow{
			ID:        s.ID,
			SourceID:  s.SourceID,
			TargetURL: s.TargetURL,
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		}
	}
	return out
}

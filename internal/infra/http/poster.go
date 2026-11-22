// Package http — EventPoster adapter for outbound webhook delivery.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// Compile-time check.
var _ application.EventPoster = (*Poster)(nil)

// Poster delivers events to subscription URLs using net/http.
type Poster struct {
	client *http.Client
}

// NewPoster constructs a Poster with the given per-request timeout.
// Per-attempt timeout — each retry attempt gets its own fresh context
// with this timeout, so a hung target can't block a worker forever.
func NewPoster(timeout time.Duration) *Poster {
	return &Poster{client: &http.Client{Timeout: timeout}}
}

// Post sends event to targetURL. Returns:
//   - nil on 2xx-3xx success
//   - application.ErrTransient on network/timeout/5xx/429/408 (retry-worthy)
//   - application.ErrPermanent on other 4xx (retry won't help)
//   - wrapped errors for malformed requests (treated as transient by caller)
//
// The retry classifier lives in the use case, not here. This adapter
// just reports WHAT happened; the use case decides WHAT TO DO.
func (p *Poster) Post(ctx context.Context, targetURL string, e *domain.Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		// Marshal failure is a programmer error, not a network issue.
		// Permanent — won't fix on retry.
		return fmt.Errorf("marshal: %w: %w", application.ErrPermanent, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		// URL parse failure is also permanent.
		return fmt.Errorf("new request: %w: %w", application.ErrPermanent, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// Network error, timeout, DNS failure, etc. → transient.
		return fmt.Errorf("do: %w: %w", application.ErrTransient, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // drain so connection can be reused

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		// 2xx success, 3xx redirect (already followed by http.Client).
		return nil

	case resp.StatusCode == http.StatusRequestTimeout, // 408
		resp.StatusCode == http.StatusTooManyRequests,     // 429
		resp.StatusCode == http.StatusInternalServerError, // 500
		resp.StatusCode == http.StatusBadGateway,          // 502
		resp.StatusCode == http.StatusServiceUnavailable,  // 503
		resp.StatusCode == http.StatusGatewayTimeout:      // 504
		return fmt.Errorf("status %d: %w", resp.StatusCode, application.ErrTransient)

	default:
		// Any other 4xx — recipient doesn't accept this payload (bad format,
		// auth, signature mismatch, etc.). Retry won't help.
		return fmt.Errorf("status %d: %w", resp.StatusCode, application.ErrPermanent)
	}
}

// Package http provides the HTTP-driven adapter for the webhook relay.
// Translates HTTP requests into application use case calls.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Sergiom16sb/webhook-relay/internal/application"
	"github.com/Sergiom16sb/webhook-relay/internal/domain"
)

// DeliveryEnqueuer schedules delivery jobs for a newly received event.
// *application.EnqueueDeliveriesUseCase satisfies it; the port keeps the
// handler testable without a worker pool.
type DeliveryEnqueuer interface {
	Enqueue(ctx context.Context, eventID string) (int, error)
}

// EventHandlers exposes HTTP endpoints for events.
// Handlers are THIN: decode request, call use case, encode response.
// All business logic lives in the use case or domain.
type EventHandlers struct {
	receiveEvent   *application.ReceiveEventUseCase
	enqueueDeliver DeliveryEnqueuer              // nil disables fan-out
	verifier       application.SignatureVerifier // nil disables signature check
}

// NewEventHandlers constructs the handler with the injected use cases.
// Pass verifier=nil to disable signature verification (dev/testing only).
// Pass enqueueDeliver=nil to receive events without fanning them out.
func NewEventHandlers(
	receive *application.ReceiveEventUseCase,
	enqueueDeliver DeliveryEnqueuer,
	verifier application.SignatureVerifier,
) *EventHandlers {
	return &EventHandlers{
		receiveEvent:   receive,
		enqueueDeliver: enqueueDeliver,
		verifier:       verifier,
	}
}

// postEventRequest is the wire format for POST /events.
type postEventRequest struct {
	ID       string          `json:"id"`
	SourceID string          `json:"source_id"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

// postEventResponse is the wire format for the response on 201 Created.
type postEventResponse struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func toResponse(e *domain.Event) postEventResponse {
	return postEventResponse{
		ID:        e.ID,
		SourceID:  e.SourceID,
		Type:      e.Type,
		Status:    string(e.Status),
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// PostEvent handles POST /events.
//
// Security: verifies X-Webhook-Signature over the raw request body
// BEFORE parsing JSON. This is the order that matters — parsing first
// would change whitespace/keys and break the signature.
//
// Status codes:
//
//	201 Created on success
//	400 Bad Request on invalid JSON or validation error
//	401 Unauthorized on missing/invalid signature
//	500 Internal Server Error on store failure
func (h *EventHandlers) PostEvent(w http.ResponseWriter, r *http.Request) {
	// Read raw body once. The verifier needs the EXACT bytes the sender
	// signed — re-encoding via Decode would normalize whitespace and break.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not read body")
		return
	}
	defer r.Body.Close()

	// Verify signature over raw bytes. Only if a verifier was injected.
	if h.verifier != nil {
		sig := r.Header.Get("X-Webhook-Signature")
		if err := h.verifier.Verify(body, sig); err != nil {
			// All signature failures collapse to 401 — no info leak
			// about WHY (bad hex vs missing vs mismatch).
			writeJSONError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	// Decode from the bytes we already read (not from r.Body again).
	var req postEventRequest
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	e, err := h.receiveEvent.Execute(r.Context(), req.ID, req.SourceID, req.Type, req.Payload)
	if err != nil {
		status, msg := mapError(err)
		writeJSONError(w, status, msg)
		return
	}

	// Enqueue delivery jobs (non-blocking — runs in worker pool).
	// Failures here are logged but don't affect the HTTP response
	// (the event is already saved and durable).
	if h.enqueueDeliver != nil {
		if _, err := h.enqueueDeliver.Enqueue(r.Context(), e.ID); err != nil {
			fmt.Printf("admin: enqueue failed for %s: %v\n", e.ID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toResponse(e))
}

// mapError converts domain/application errors to HTTP status codes.
// Keeps status mapping here (transport concern), not in the use case.
func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrEmptyID),
		errors.Is(err, domain.ErrEmptySourceID),
		errors.Is(err, domain.ErrEmptyType),
		errors.Is(err, domain.ErrEmptyPayload):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

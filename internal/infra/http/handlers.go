// Package http provides the HTTP-driven adapter for the webhook relay.
// Translates HTTP requests into application use case calls.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
)

// EventHandlers exposes HTTP endpoints for events.
// Handlers are THIN: decode request, call use case, encode response.
// All business logic lives in the use case or domain.
type EventHandlers struct {
	receiveEvent *application.ReceiveEventUseCase
}

// NewEventHandlers constructs the handler with the injected use case.
func NewEventHandlers(receive *application.ReceiveEventUseCase) *EventHandlers {
	return &EventHandlers{receiveEvent: receive}
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
// Body:  {"id":"...", "source_id":"...", "type":"...", "payload":{...}}
// Status:
//
//	201 Created on success
//	400 Bad Request on invalid JSON or validation error
//	500 Internal Server Error on store failure
func (h *EventHandlers) PostEvent(w http.ResponseWriter, r *http.Request) {
	var req postEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	e, err := h.receiveEvent.Execute(r.Context(), req.ID, req.SourceID, req.Type, req.Payload)
	if err != nil {
		status, msg := mapError(err)
		writeJSONError(w, status, msg)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toResponse(e))
}

// mapError converts domain errors to HTTP status codes.
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

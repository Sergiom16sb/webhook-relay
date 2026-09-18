package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// validEvent returns an Event that passes Validate.
func validEvent() Event {
	return Event{
		ID:        "evt_123",
		SourceID:  "src_shopify",
		Type:      "order.created",
		Payload:   json.RawMessage(`{"order_id":"ord_42","total":99.95}`),
		Status:    EventStatusPending,
		CreatedAt: time.Date(2026, 9, 17, 22, 0, 0, 0, time.UTC),
	}
}

func TestEvent_Roundtrip(t *testing.T) {
	// JSON round-trip must preserve every field.
	original := validEvent()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.SourceID != original.SourceID {
		t.Errorf("SourceID: got %q, want %q", decoded.SourceID, original.SourceID)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", decoded.Payload, original.Payload)
	}
}

func TestEvent_JSONFieldNames(t *testing.T) {
	// Struct tags must use snake_case for API stability.
	e := validEvent()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}

	required := []string{
		`"id"`,
		`"source_id"`,
		`"type"`,
		`"payload"`,
		`"status"`,
		`"created_at"`,
	}
	for _, key := range required {
		if !strings.Contains(string(data), key) {
			t.Errorf("JSON missing field %s in %s", key, data)
		}
	}
}

func TestEvent_PayloadAsRawMessage(t *testing.T) {
	// Payload is json.RawMessage — must survive opaque bytes without re-parsing.
	original := json.RawMessage([]byte(`{"a":1,"nested":{"b":"two","c":[3,4]}}`))
	e := Event{
		ID:        "e1",
		SourceID:  "s1",
		Type:      "t1",
		Payload:   original,
		CreatedAt: time.Now().UTC(),
	}

	data, _ := json.Marshal(e)
	var decoded Event
	json.Unmarshal(data, &decoded)

	if string(decoded.Payload) != string(original) {
		t.Errorf("payload bytes changed: got %s, want %s", decoded.Payload, original)
	}
}

func TestEvent_TimeFormat(t *testing.T) {
	// time.Time serializes as RFC3339Nano by default — verify and document.
	e := validEvent()
	data, _ := json.Marshal(e)

	if !strings.Contains(string(data), `"created_at":"2026-09-17T22:00:00Z"`) {
		t.Errorf("time format unexpected: %s", data)
	}
}

func TestEventStatus_IsTerminal(t *testing.T) {
	cases := []struct {
		status EventStatus
		want   bool
	}{
		{EventStatusPending, false},
		{EventStatusDelivered, true},
		{EventStatusFailed, true},
	}
	for _, tt := range cases {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestEvent_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(e *Event)
		wantErr error
	}{
		{"valid", func(e *Event) {}, nil},
		{"empty id", func(e *Event) { e.ID = "" }, ErrEmptyID},
		{"empty source", func(e *Event) { e.SourceID = "" }, ErrEmptySourceID},
		{"empty type", func(e *Event) { e.Type = "" }, ErrEmptyType},
		{"empty payload", func(e *Event) { e.Payload = nil }, ErrEmptyPayload},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent()
			tt.mutate(&e)
			err := e.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvent_String(t *testing.T) {
	e := validEvent()
	got := e.String()
	for _, want := range []string{"evt_123", "src_shopify", "order.created", "pending"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}

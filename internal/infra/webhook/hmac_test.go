package webhook_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/infra/webhook"
)

func newVerifier(t *testing.T) *webhook.HMACVerifier {
	t.Helper()
	v, err := webhook.NewHMACVerifier("test-secret-key-do-not-use-in-prod")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

func TestHMACVerifier_SignAndVerify(t *testing.T) {
	v := newVerifier(t)
	payload := []byte(`{"event":"order.created","id":"evt_1"}`)

	sig := v.Sign(payload)
	if err := v.Verify(payload, sig); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestHMACVerifier_RejectsTamperedPayload(t *testing.T) {
	v := newVerifier(t)
	original := []byte(`{"amount":100}`)
	sig := v.Sign(original)

	// Attacker tampers with the payload (e.g., changes amount).
	tampered := []byte(`{"amount":1000000}`)

	if err := v.Verify(tampered, sig); err == nil {
		t.Error("tampered payload accepted — signature verification is broken")
	}
}

func TestHMACVerifier_RejectsWrongSignature(t *testing.T) {
	v := newVerifier(t)
	payload := []byte(`{"data":"x"}`)

	// Sign with a different secret, then try to verify.
	other, _ := webhook.NewHMACVerifier("other-secret")
	wrongSig := other.Sign(payload)

	if err := v.Verify(payload, wrongSig); err == nil {
		t.Error("wrong-secret signature accepted")
	}
}

func TestHMACVerifier_EmptySignature(t *testing.T) {
	v := newVerifier(t)
	if err := v.Verify([]byte(`{"data":"x"}`), ""); err == nil {
		t.Error("empty signature accepted — must reject")
	} else if !errors.Is(err, application.ErrInvalidSignature) {
		t.Errorf("got %v, want ErrInvalidSignature", err)
	}
}

func TestHMACVerifier_MalformedHex(t *testing.T) {
	v := newVerifier(t)
	if err := v.Verify([]byte(`{}`), "not-hex!!!"); err == nil {
		t.Error("malformed hex accepted")
	} else if !errors.Is(err, application.ErrInvalidSignature) {
		t.Errorf("got %v, want ErrInvalidSignature", err)
	}
}

func TestHMACVerifier_WrongLength(t *testing.T) {
	// SHA-256 produces 32 bytes → 64 hex chars. Anything else fails
	// hmac.Equal (different lengths → false, not timing-leak).
	v := newVerifier(t)
	shortSig := strings.Repeat("ab", 10) // 20 chars, invalid
	if err := v.Verify([]byte(`{}`), shortSig); err == nil {
		t.Error("wrong-length signature accepted")
	}
}

func TestNewHMACVerifier_RejectsEmptySecret(t *testing.T) {
	if _, err := webhook.NewHMACVerifier(""); err == nil {
		t.Error("empty secret accepted — verifier would match any signature")
	}
}

// TestHMACVerifier_ConstantTimeNotBytesEqual is a sanity check that we
// use hmac.Equal (constant-time), not bytes.Equal (timing-leaky).
// This is enforced by code review + the import of crypto/hmac; we don't
// grep the source here because that's brittle.

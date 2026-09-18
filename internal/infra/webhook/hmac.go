// Package webhook provides HTTP-edge adapters for the webhook relay:
// HMAC signature verification, HTTP delivery clients, etc.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/sgtour/webhook-relay/internal/application"
)

// Compile-time check: HMACVerifier satisfies application.SignatureVerifier.
// If you remove or rename Verify, build fails here.
var _ application.SignatureVerifier = (*HMACVerifier)(nil)

// HMACVerifier validates HMAC-SHA256 signatures over request payloads.
// The signature is sent as a hex-encoded string in the X-Webhook-Signature
// header. Algorithm: HMAC-SHA256(secret, raw_body) → hex.
//
// Production pattern: receiver computes HMAC over the EXACT raw bytes of
// the request body (before any JSON parsing). Sender does the same on their
// end. If the bytes change in transit or parsing, signatures mismatch.
type HMACVerifier struct {
	secret []byte
}

// NewHMACVerifier constructs a verifier with the given shared secret.
// Empty secret is rejected — verifier would accept any signature.
func NewHMACVerifier(secret string) (*HMACVerifier, error) {
	if secret == "" {
		return nil, errors.New("hmac: empty secret")
	}
	return &HMACVerifier{secret: []byte(secret)}, nil
}

// Verify returns nil if signature is a valid HMAC of payload, an error
// otherwise. Uses hmac.Equal for constant-time comparison (prevents
// timing attacks that leak bytes one-at-a-time).
//
// Distinguishes three failure modes:
//   - empty signature → ErrInvalidSignature (caller probably forgot header)
//   - malformed hex   → ErrInvalidSignature
//   - mismatch        → ErrInvalidSignature
//
// All three surface as 401 to the caller (no info leak about WHY).
func (v *HMACVerifier) Verify(payload []byte, signature string) error {
	if signature == "" {
		return fmt.Errorf("hmac: %w (empty)", application.ErrInvalidSignature)
	}

	// Decode the hex signature from the header.
	got, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("hmac: %w (bad hex: %v)", application.ErrInvalidSignature, err)
	}

	// Compute the expected HMAC.
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	want := mac.Sum(nil)

	// Constant-time compare. NEVER use bytes.Equal — leaks timing info.
	if !hmac.Equal(got, want) {
		return fmt.Errorf("hmac: %w (mismatch)", application.ErrInvalidSignature)
	}
	return nil
}

// Sign is a helper for clients/tests: compute the signature for a payload.
// Not part of the SignatureVerifier port — production code doesn't sign;
// only senders do.
func (v *HMACVerifier) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

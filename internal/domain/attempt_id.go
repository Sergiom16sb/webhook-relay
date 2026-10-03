package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// AttemptID returns a deterministic, collision-resistant ID for a
// (event, subscription) delivery attempt row. Same inputs → same ID,
// across processes and restarts. Stable so re-enqueues (e.g. after a
// crash + recovery) target the same row, not a new one.
//
// 128 bits of SHA-256 — collision probability is negligible for any
// realistic event volume. Hex-encoded = 32 chars, fits in TEXT PK.
func AttemptID(eventID, subscriptionID string) string {
	sum := sha256.Sum256([]byte(eventID + "\x00" + subscriptionID))
	return hex.EncodeToString(sum[:16])
}

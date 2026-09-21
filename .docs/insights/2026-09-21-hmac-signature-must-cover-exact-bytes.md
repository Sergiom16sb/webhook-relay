# Insight: HMAC signature must cover exact bytes

**Date**: 2026-09-21
**Context**: Step 1.18 E2E test — the idempotency test computed the signature over one set of bytes but POSTed different bytes. First POST returned 201; second POST returned 401 because the signature didn't match the body.

**Insight**: When computing an HMAC signature in tests, the bytes you sign MUST be the exact bytes you POST. Re-computing inside the test (over the actual body) is the only safe pattern.

**Anti-pattern**:
```go
payload := []byte(`{"id":"first",...}`)
sig := signPayload(secret, payload)  // signed once

post := func(id string) int {
    body := []byte(fmt.Sprintf(`{"id":%q,...}`, id))  // different bytes
    req.Header.Set("X-Webhook-Signature", sig)         // sig doesn't match body
    ...
}
```

**Correct pattern**:
```go
post := func(id string) int {
    body := []byte(fmt.Sprintf(`{"id":%q,...}`, id))
    req.Header.Set("X-Webhook-Signature", signPayload(secret, body))  // sign actual body
    ...
}
```

This applies to all HMAC testing — the verifier checks bytes, not "the general shape of the request." Off-by-one whitespace changes break signatures.

**Skill update candidate**: `go-testing` (small note), or `clean-architecture` (webhook-specific subsection).

**Where fixed in code**: `internal/integration/e2e_test.go` `TestE2E_Idempotency_SamePayloadOnce` (commit 328f94e).

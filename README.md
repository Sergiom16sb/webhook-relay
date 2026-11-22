# webhook-relay

Self-hosted webhook receiver and relay with HMAC verification, idempotent storage, retry-with-backoff delivery, and an admin UI.

## What it does

- Receives webhooks (POST `/events`) with HMAC-SHA256 signature verification over the exact raw body
- Stores events idempotently by `(source_id, payload_hash)` — duplicate POSTs return the same event ID
- Delivers each event to every active subscription for its `source_id` through a bounded worker pool, retrying transient failures with exponential backoff + jitter (7 attempts by default, 60s cap)
- Classifies delivery failures: network/408/429/5xx are retried, other 4xx fail immediately; the attempt row is recorded as `failed` and the event can be retried from the admin UI
- Exposes an admin UI at `/admin*` (templ + htmx + daisyUI) for dashboards, event inspection, manual retry, subscription CRUD, and HMAC signature generation

## Quick start

### With Docker Compose (recommended)

```bash
export WEBHOOK_SECRET=test-secret
docker compose up -d
# Wait ~5s for postgres + api to start

SECRET=test-secret
PAYLOAD='{"id":"e1","source_id":"src","type":"order.created","payload":{"k":"v"}}'
SIG=$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')

curl -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -H "X-Webhook-Signature: $SIG" \
  -d "$PAYLOAD"
# → 201 Created

# Browse admin panel
open http://localhost:8080/admin
```

### Without Docker (in-memory store)

```bash
export WEBHOOK_SECRET=test-secret
go run ./cmd/api
# Listens on :8080 with memory store. Restart = data lost. Dev only.
```

If port 5432 on your host is already in use (e.g. another postgres container), the compose file maps webhook postgres to **5433** on host. Adjust the api's `DATABASE_URL` env var if connecting from outside the compose network.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Returns `hello`. Healthcheck endpoint. |
| POST | `/events` | Receive webhook. Requires `X-Webhook-Signature` header (HMAC-SHA256 hex over raw body). |
| GET | `/admin` | Dashboard with KPIs + recent events |
| GET | `/admin/events` | List events with filters (status, source, search) |
| GET | `/admin/events/{id}` | Event detail (payload, retry button) |
| POST | `/admin/events/{id}/retry` | Manual retry (resets status to pending) |
| GET | `/admin/hmac` | HMAC signature generator (dev tool) |
| POST | `/admin/hmac` | Compute signature for given payload |
| GET | `/admin/subscriptions` | List subscriptions |
| POST | `/admin/subscriptions` | Create subscription (`source_id`, `target_url`, `secret`) |
| POST | `/admin/subscriptions/{id}/delete` | Soft-delete subscription (`status = deleted`) |

### Event payload format

```json
{
  "id": "evt_unique_id",
  "source_id": "shopify",
  "type": "order.created",
  "payload": { "any": "json" }
}
```

`id` is caller-provided. The pair `(source_id, payload)` (more precisely `(source_id, payload_hash)`) is the idempotency key — duplicate POSTs return the same event ID.

## Architecture

Hexagonal / Clean Architecture:

```
cmd/api/main.go                  # composition root (only file knowing all types)
internal/
  domain/                        # entities, value objects, errors
  application/                   # use cases + ports (consumer-side interfaces)
  infra/
    http/                        # HTTP adapter: handlers + middleware (hmac, logger, recovery)
    postgres/                    # Postgres adapter: sqlx + testcontainers-tested
    memory/                      # in-memory adapter: dev mode
    webhook/                     # HMAC-SHA256 verifier
    worker/                      # bounded worker pool with ctx cancel
    admin/                       # htmx + DaisyUI admin panel (templ)
    logger/                      # context-aware slog propagation
internal/integration/            # E2E tests with -tags=integration
```

The composition root is the only place that knows concrete types; use cases depend on the ports they declare in `internal/application/ports.go`, and adapters bind structurally (no shared interface package).

## Tests

```bash
# Unit + adapter tests. The Postgres adapter tests spin up real Postgres
# via testcontainers, so Docker must be running (~40s total).
go test ./...

# End-to-end tests with real Postgres via testcontainers (~40s)
go test -tags=integration ./internal/integration/...

# Format + vet
gofmt -l . && go vet ./... && go vet -tags=integration ./internal/integration/...
```

E2E coverage:
- HTTP happy path (POST + persist)
- HMAC invalid signature (401)
- Idempotency (same payload 2x → 1 event)
- Delivery happy path
- Delivery retry then success
- Delivery exhausts max attempts → `ErrMaxAttemptsExceeded`
- Full pipeline (POST → receive → persist → deliver)

## Configuration

| Env var | Default | Description |
|---------|---------|-------------|
| `WEBHOOK_SECRET` | (empty) | HMAC secret. Empty = signature verification disabled. |
| `DATABASE_URL` | (empty) | Postgres connection. Empty = in-memory store. |
| `PORT` | `:8080` | Listen port. Accepts `8080` or `:8080`; the Docker HEALTHCHECK follows it. |

## Roadmap

- **v1**: receive, verify, persist, deliver with retry, admin UI, Docker compose, E2E tests
- **v2 (in progress)**: subscriptions CRUD, delivery-attempt tracking in the admin UI — done; SSE live dashboard, source management, daisyUI v5 + Tailwind 4 upgrade — pending

## License

MIT — see [LICENSE](LICENSE).

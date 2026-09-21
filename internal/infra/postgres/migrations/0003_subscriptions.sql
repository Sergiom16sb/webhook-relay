-- 0003_subscriptions.sql
-- Subscription routing + per-event delivery attempts.

CREATE TABLE IF NOT EXISTS subscriptions (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL,
    target_url  TEXT NOT NULL,
    secret      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_id, target_url)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_source_id
    ON subscriptions (source_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id              TEXT PRIMARY KEY,
    event_id        TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    attempt_num     INT NOT NULL,
    status          TEXT NOT NULL,
    status_code     INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    UNIQUE (event_id, subscription_id, attempt_num)
);

CREATE INDEX IF NOT EXISTS idx_delivery_attempts_event_id
    ON delivery_attempts (event_id, attempt_num);

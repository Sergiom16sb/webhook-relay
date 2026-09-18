-- 0001_events.sql
-- Initial schema for the webhook relay.
-- Idempotent: safe to run multiple times.

CREATE TABLE IF NOT EXISTS events (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL,
    type        TEXT NOT NULL,
    payload     JSONB NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_source_id ON events (source_id);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at DESC);

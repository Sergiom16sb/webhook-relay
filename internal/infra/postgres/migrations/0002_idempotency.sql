-- 0002_idempotency.sql
-- Add payload_hash column for idempotency keys.
-- Same (source_id, payload_hash) tuple = duplicate event. Return existing
-- instead of inserting new.
--
-- Uses a UNIQUE CONSTRAINT (not just an index) so ON CONFLICT works in
-- the Save upsert. Partial constraint: NULL payload_hash (not yet computed)
-- is not constrained — only computed hashes must be unique.

ALTER TABLE events ADD COLUMN IF NOT EXISTS payload_hash TEXT;

-- Drop the partial unique index if it exists from a previous version of this migration.
DROP INDEX IF EXISTS uq_events_source_hash;

-- Add a proper UNIQUE constraint that's recognized by ON CONFLICT.
ALTER TABLE events
    ADD CONSTRAINT uq_events_source_hash
    UNIQUE (source_id, payload_hash);

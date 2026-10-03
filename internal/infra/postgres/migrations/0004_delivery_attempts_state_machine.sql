-- 0004_delivery_attempts_state_machine.sql
-- Convert delivery_attempts from append-only log (one row per attempt_num)
-- to single-row state machine per (event, subscription). One row per
-- (event_id, subscription_id), status transitions:
--   pending → success | failed | retrying
--
-- Why: append-only created "ghost" pending rows that never updated
-- (because the executor used a different attemptID than the enqueue
-- use case), so the admin dashboard would see both a pending AND a
-- success row for the same logical delivery. Single-row upsert makes
-- status the single source of truth and the dashboard query trivial.
--
-- attempt_count: how many HTTP POSTs the delivery use case made
-- (incremented on each retry inside deliver_event). Initial value 1
-- (the enqueue itself counts as the first attempt being scheduled).
-- NOTE: v1 does not plumb this through the retry loop — it's always 1
-- until we wire per-attempt updates. The column is forward-looking.
--
-- attempt_num is kept in the schema (always 1 now) for backward compat
-- with existing rows; vestigial.

-- Drop the old (event_id, subscription_id, attempt_num) unique constraint.
-- The auto-generated name is `delivery_attempts_event_id_subscription_id_attempt_num_key`.
ALTER TABLE delivery_attempts
    DROP CONSTRAINT IF EXISTS delivery_attempts_event_id_subscription_id_attempt_num_key;

-- New constraint: one row per (event, subscription).
ALTER TABLE delivery_attempts
    ADD CONSTRAINT uq_delivery_attempts_event_sub
    UNIQUE (event_id, subscription_id);

-- Forward-looking column for retry counts. NOT plumbed yet.
ALTER TABLE delivery_attempts
    ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 1;

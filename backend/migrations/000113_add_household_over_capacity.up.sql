-- #1371: Phase 7 — Household Offboarding & Lifecycle: over-capacity grace period.
--
-- Adds the schema needed to track a household that has fallen over its plan's
-- seat limit (e.g. after a downgrade) and is running a 14-day grace clock before
-- a background worker auto-offboards excess members. No default/backfill: this
-- state does not exist for any pre-existing household row.

ALTER TABLE households ADD COLUMN IF NOT EXISTS over_capacity_since TIMESTAMPTZ;
ALTER TABLE households ADD COLUMN IF NOT EXISTS over_capacity_start_notice_sent_at TIMESTAMPTZ;
ALTER TABLE households ADD COLUMN IF NOT EXISTS over_capacity_warn3d_sent_at TIMESTAMPTZ;

COMMENT ON COLUMN households.over_capacity_since IS
    'Grace-period anchor timestamp. When set, the household currently has more '
    'active members than its plan''s seat limit allows, and a 14-day grace clock '
    'is running before a background worker auto-offboards the most-recently-joined '
    'excess members. NULL when the household is within its seat limit.';

COMMENT ON COLUMN households.over_capacity_start_notice_sent_at IS
    'Idempotency marker: when set, the "you''re over capacity" warning email has '
    'already been sent for the current grace period. Prevents duplicate sends.';

COMMENT ON COLUMN households.over_capacity_warn3d_sent_at IS
    'Idempotency marker: when set, the "3 days left" warning email has already '
    'been sent for the current grace period. Prevents duplicate sends.';

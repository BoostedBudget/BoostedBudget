-- 000219_backfill_legacy_manual_tier_override.down.sql
--
-- This repair is irreversible: the pre-repair manual_tier_override/manual_tier_ends_at values are
-- not recorded anywhere, so there is nothing to restore. The statement below is a deliberate no-op
-- that still carries its own WHERE clause, matching the repo's convention that a DOWN migration
-- must be bounded (see db-migration-guide § Rollback Patterns and the migration-history-delete
-- guard's DOWN exemption).
--
-- If a specific environment genuinely needs to re-run the repair, re-apply the up migration — it is
-- idempotent. Never hand-restore a stale override: it would re-block the user's retention pipeline.
UPDATE users
SET manual_tier_override = manual_tier_override
WHERE FALSE;
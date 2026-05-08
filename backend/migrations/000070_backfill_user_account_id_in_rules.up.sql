-- Migration: Backfill user_account_id keys in rules JSONB columns
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: Enrich rules.actions and rules.trigger_config JSONB with stable
--          *_user_account_id keys ALONGSIDE the existing *_account_id keys.
--          Original keys are preserved so this migration is safely reversible.
--
-- rules.actions shape (transfer_suggestion rules):
--   {"from_account_id": "uuid", "to_account_id": "uuid", "amount_type": "...", "amount": N}
-- rules.trigger_config.filters shape:
--   {"type": "event", "filters": {"account_id": "uuid", ...}}
--
-- Idempotency: each UPDATE only touches rows where the new key is not yet present.

-- =============================================================================
-- Step 1: Backfill from_user_account_id into rules.actions
--         Only processes transfer_suggestion rules that have from_account_id.
-- =============================================================================

UPDATE rules r
SET actions = r.actions || jsonb_build_object('from_user_account_id', a.user_account_id::text)
FROM accounts a
WHERE r.rule_type = 'transfer_suggestion'
  AND r.actions ? 'from_account_id'
  AND NOT (r.actions ? 'from_user_account_id')
  AND a.id::text = r.actions->>'from_account_id'
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 2: Backfill to_user_account_id into rules.actions
--         Only processes transfer_suggestion rules that have to_account_id.
-- =============================================================================

UPDATE rules r
SET actions = r.actions || jsonb_build_object('to_user_account_id', a.user_account_id::text)
FROM accounts a
WHERE r.rule_type = 'transfer_suggestion'
  AND r.actions ? 'to_account_id'
  AND NOT (r.actions ? 'to_user_account_id')
  AND a.id::text = r.actions->>'to_account_id'
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 3: Backfill user_account_id into rules.trigger_config filters
--         Only processes event-type triggers that have an account_id filter key.
-- =============================================================================

UPDATE rules r
SET trigger_config = jsonb_set(
    r.trigger_config,
    '{filters}',
    r.trigger_config->'filters' || jsonb_build_object('user_account_id', a.user_account_id::text)
)
FROM accounts a
WHERE r.trigger_config->>'type' = 'event'
  AND r.trigger_config->'filters' IS NOT NULL
  AND r.trigger_config->'filters' ? 'account_id'
  AND NOT (r.trigger_config->'filters' ? 'user_account_id')
  AND a.id::text = r.trigger_config->'filters'->>'account_id'
  AND a.user_account_id IS NOT NULL;

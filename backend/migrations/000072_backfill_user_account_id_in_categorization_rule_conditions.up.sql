-- Migration: Backfill user_account_id in categorization_rules.conditions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: The AccountIDEvaluator (internal/rules/evaluator.go) now prefers
--          tx.UserAccountID over tx.AccountID. Condition values for type='account_id'
--          must be stable user_account_id UUIDs so they keep matching after a re-link.
--          This migration replaces raw account.id values with the corresponding
--          accounts.user_account_id for all conditions where type='account_id'.
--
-- Idempotency: The lookup `WHERE accounts.id::text = value` only succeeds when
--              the stored value is a raw account.id UUID. Once replaced by a
--              user_account_id UUID (which lives in user_accounts, not accounts),
--              the lookup finds nothing and the value is left unchanged.
--
-- Condition JSONB shape:
--   conditions: [{"type": "account_id", "operator": "...", "value": "uuid-or-array"}, ...]

-- =============================================================================
-- Step 1: Backfill string-valued account_id conditions
--         e.g. {"type": "account_id", "value": "<raw-account-uuid>"}
-- =============================================================================

UPDATE categorization_rules cr
SET conditions = (
    SELECT COALESCE(jsonb_agg(
        CASE
            WHEN (cond->>'type') = 'account_id'
                AND jsonb_typeof(cond->'value') = 'string'
            THEN jsonb_set(
                cond, '{value}',
                COALESCE(
                    (SELECT to_jsonb(a.user_account_id::text)
                     FROM accounts a
                     WHERE a.id::text = cond->>'value'
                       AND a.user_account_id IS NOT NULL
                     LIMIT 1),
                    cond->'value'  -- keep as-is when no mapping found or already migrated
                )
            )
            ELSE cond
        END
    ), '[]'::jsonb)
    FROM jsonb_array_elements(cr.conditions) AS cond
)
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(cr.conditions) AS c
    WHERE c->>'type' = 'account_id'
      AND jsonb_typeof(c->'value') = 'string'
      AND EXISTS (
          SELECT 1 FROM accounts a
          WHERE a.id::text = c->>'value'
            AND a.user_account_id IS NOT NULL
      )
);

-- =============================================================================
-- Step 2: Backfill array-valued account_id conditions
--         e.g. {"type": "account_id", "value": ["uuid1", "uuid2"]}
-- =============================================================================

UPDATE categorization_rules cr
SET conditions = (
    SELECT COALESCE(jsonb_agg(
        CASE
            WHEN (cond->>'type') = 'account_id'
                AND jsonb_typeof(cond->'value') = 'array'
            THEN jsonb_set(
                cond, '{value}',
                (
                    SELECT COALESCE(jsonb_agg(
                        COALESCE(
                            (SELECT to_jsonb(a.user_account_id::text)
                             FROM accounts a
                             WHERE a.id::text = elem#>>'{}'
                               AND a.user_account_id IS NOT NULL
                             LIMIT 1),
                            elem
                        )
                    ), '[]'::jsonb)
                    FROM jsonb_array_elements(cond->'value') AS elem
                )
            )
            ELSE cond
        END
    ), '[]'::jsonb)
    FROM jsonb_array_elements(cr.conditions) AS cond
)
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(cr.conditions) AS c
    WHERE c->>'type' = 'account_id'
      AND jsonb_typeof(c->'value') = 'array'
      AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements(c->'value') AS v,
               accounts a
          WHERE a.id::text = v#>>'{}'
            AND a.user_account_id IS NOT NULL
      )
);

-- Rollback: Restore raw account_id values in categorization_rules.conditions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
--
-- NOTE: This rollback re-maps user_account_id values back to their corresponding
-- active account.id. If the account was re-linked after the up migration ran,
-- this will restore the NEW (post-relink) account.id, not the original pre-relink
-- account.id — which is the correct behaviour (the active row's id is the one
-- that matters). Rows whose user_account_id has no active account are left as-is.

-- =============================================================================
-- Step 1: Restore string-valued conditions
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
                    (SELECT to_jsonb(a.id::text)
                     FROM accounts a
                     WHERE a.user_account_id::text = cond->>'value'
                       AND a.status = 'active'
                     LIMIT 1),
                    cond->'value'
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
          WHERE a.user_account_id::text = c->>'value'
            AND a.status = 'active'
      )
);

-- =============================================================================
-- Step 2: Restore array-valued conditions
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
                            (SELECT to_jsonb(a.id::text)
                             FROM accounts a
                             WHERE a.user_account_id::text = elem#>>'{}'
                               AND a.status = 'active'
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
          WHERE a.user_account_id::text = v#>>'{}'
            AND a.status = 'active'
      )
);

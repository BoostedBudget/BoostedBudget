-- #1334: Restore logical uniqueness on user_accounts via blind-index columns.
--
-- Background: migration 000031 added UNIQUE (user_id, institution_id, mask).
-- Migration 000053 encrypted institution_id and mask with AES-GCM (random nonce),
-- making every ciphertext unique — so the old constraint now enforces ciphertext
-- uniqueness, not logical identity. 000053 also added blind-index columns
-- (institution_id_blind_idx / mask_blind_idx, HMAC-SHA256, deterministic) and a
-- non-unique index idx_user_accounts_inst_mask_blind on those columns.
--
-- This migration:
--   1. Preflights for inconsistent blind-index state.
--   2. Merges customizations from loser rows into the survivor.
--   3. Re-points JSONB user_account_id references in boosts (loser → survivor).
--   4. Re-points JSONB user_account_id references in categorization_rules (loser → survivor).
--   5. Re-points 8 child FK columns from loser → survivor.
--   6. Deletes loser rows.
--   7. Drops the old non-unique index and creates a NULLS NOT DISTINCT unique index.
--
-- NOTE: No temp tables — psql -f - runs in autocommit. All loser/survivor
-- mapping is done via inlined CTEs repeated verbatim in each statement.

-- =============================================================================
-- Step 1: Preflight — abort on any un-backfilled or inconsistent blind-index state
--
-- Guard 1: Abort if ANY row has NULL institution_id_blind_idx (un-backfilled).
--          Such rows are invisible to FindByInstitutionAndMask and excluded from
--          both dedup and the unique index — run cmd/encrypt-migrate first.
-- Guard 2: Abort if any indexed row has mask but NULL mask_blind_idx (inconsistent
--          partial backfill — would cause wrong NULL-mask merge).
-- Guard 3: Abort if any row has mask=NULL but mask_blind_idx IS NOT NULL (inverse
--          inconsistency — data integrity anomaly).
-- =============================================================================

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM user_accounts WHERE institution_id_blind_idx IS NULL) THEN
    RAISE EXCEPTION '#1334: un-backfilled user_accounts (NULL institution_id_blind_idx); run cmd/encrypt-migrate --table user_accounts first';
  END IF;
  IF EXISTS (
    SELECT 1 FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
      AND mask IS NOT NULL
      AND mask_blind_idx IS NULL
  ) THEN
    RAISE EXCEPTION '#1334: user_accounts rows have mask but NULL mask_blind_idx; run cmd/encrypt-migrate --table user_accounts first';
  END IF;
  IF EXISTS (SELECT 1 FROM user_accounts WHERE mask IS NULL AND mask_blind_idx IS NOT NULL) THEN
    RAISE EXCEPTION '#1334: user_accounts rows have NULL mask but non-NULL mask_blind_idx; data inconsistency — fix before migrating';
  END IF;
END $$;

-- =============================================================================
-- Step 2: Merge customizations into survivor
-- For each duplicate group (same user_id, institution_id_blind_idx, mask_blind_idx),
-- pick the oldest row (created_at ASC, id ASC) as the survivor and copy the most-recent
-- non-null customization fields to it. is_hidden = TRUE only if ALL members are hidden.
-- =============================================================================

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
agg AS (
    SELECT
        d.survivor_id,
        (array_remove(array_agg(ua.nickname          ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS nickname,
        (array_remove(array_agg(ua.custom_color      ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS custom_color,
        (array_remove(array_agg(ua.custom_icon       ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS custom_icon,
        (array_remove(array_agg(ua.custom_emoji      ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS custom_emoji,
        (array_remove(array_agg(ua.account_name_hint ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS account_name_hint,
        (array_remove(array_agg(ua.primary_type      ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS primary_type,
        (array_remove(array_agg(ua.primary_subtype   ORDER BY ua.updated_at DESC NULLS LAST, ua.created_at DESC, ua.id DESC), NULL))[1] AS primary_subtype,
        bool_and(ua.is_hidden) AS all_hidden
    FROM dupes d
    JOIN user_accounts ua ON ua.id = d.id
    GROUP BY d.survivor_id
    HAVING count(*) > 1
)
UPDATE user_accounts ua
SET
    nickname          = COALESCE(ua.nickname,          agg.nickname),
    custom_color      = COALESCE(ua.custom_color,      agg.custom_color),
    custom_icon       = COALESCE(ua.custom_icon,       agg.custom_icon),
    custom_emoji      = COALESCE(ua.custom_emoji,      agg.custom_emoji),
    account_name_hint = COALESCE(ua.account_name_hint, agg.account_name_hint),
    primary_type      = COALESCE(ua.primary_type,      agg.primary_type),
    primary_subtype   = COALESCE(ua.primary_subtype,   agg.primary_subtype),
    is_hidden         = agg.all_hidden,
    updated_at        = now()
FROM agg
WHERE ua.id = agg.survivor_id;

-- =============================================================================
-- Step 3: Rewrite boosts JSONB — loser → survivor (3 statements)
-- =============================================================================

-- 3a. boosts.actions.transfer.from_user_account_id
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,from_user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'from_user_account_id' = l.loser_id::text;

-- 3b. boosts.actions.transfer.to_user_account_id
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,to_user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'to_user_account_id' = l.loser_id::text;

-- 3c. boosts.trigger_config.filters.user_account_id
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET trigger_config = jsonb_set(b.trigger_config, '{filters,user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.trigger_config->>'type' = 'event'
  AND b.trigger_config->'filters'->>'user_account_id' = l.loser_id::text;

-- =============================================================================
-- Step 4: Rewrite categorization_rules.conditions — loser → survivor (2 statements)
-- =============================================================================

-- 4a. String-valued account_id conditions: {"type": "account_id", "value": "<uuid>"}
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE categorization_rules cr
SET conditions = (
    SELECT COALESCE(jsonb_agg(
        CASE
            WHEN (cond->>'type') = 'account_id'
                AND jsonb_typeof(cond->'value') = 'string'
            THEN jsonb_set(
                cond, '{value}',
                COALESCE(
                    (SELECT to_jsonb(l2.survivor_id::text)
                     FROM losers l2
                     WHERE l2.loser_id::text = cond->>'value'
                     LIMIT 1),
                    cond->'value'
                )
            )
            ELSE cond
        END
    ), '[]'::jsonb)
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(cr.conditions) = 'array' THEN cr.conditions ELSE '[]'::jsonb END
    ) AS cond
)
WHERE jsonb_typeof(cr.conditions) = 'array'
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(cr.conditions) = 'array' THEN cr.conditions ELSE '[]'::jsonb END
    ) AS c,
         losers l3
    WHERE c->>'type' = 'account_id'
      AND jsonb_typeof(c->'value') = 'string'
      AND l3.loser_id::text = c->>'value'
  );

-- 4b. Array-valued account_id conditions: {"type": "account_id", "value": ["uuid1", ...]}
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
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
                            (SELECT to_jsonb(l2.survivor_id::text)
                             FROM losers l2
                             WHERE l2.loser_id::text = elem_text
                             LIMIT 1),
                            to_jsonb(elem_text)
                        )
                    ), '[]'::jsonb)
                    FROM jsonb_array_elements_text(cond->'value') AS elem_text
                )
            )
            ELSE cond
        END
    ), '[]'::jsonb)
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(cr.conditions) = 'array' THEN cr.conditions ELSE '[]'::jsonb END
    ) AS cond
)
WHERE jsonb_typeof(cr.conditions) = 'array'
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(cr.conditions) = 'array' THEN cr.conditions ELSE '[]'::jsonb END
    ) AS c
    WHERE c->>'type' = 'account_id'
      AND jsonb_typeof(c->'value') = 'array'
      AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(c->'value') AS v_text, losers l3
          WHERE l3.loser_id::text = v_text
      )
  );

-- =============================================================================
-- Step 5: Re-point 8 child FK columns — loser → survivor
-- =============================================================================

-- 5.1 accounts.user_account_id (ON DELETE RESTRICT — must re-point before deleting losers)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE accounts a
SET user_account_id = l.survivor_id
FROM losers l
WHERE a.user_account_id = l.loser_id;

-- 5.2 transactions.user_account_id (ON DELETE RESTRICT — must re-point before deleting losers)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE transactions t
SET user_account_id = l.survivor_id
FROM losers l
WHERE t.user_account_id = l.loser_id;

-- 5.3 balance_snapshots.user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE balance_snapshots bs
SET user_account_id = l.survivor_id
FROM losers l
WHERE bs.user_account_id = l.loser_id;

-- 5.4 savings_goals.user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE savings_goals sg
SET user_account_id = l.survivor_id
FROM losers l
WHERE sg.user_account_id = l.loser_id;

-- 5.5 transfer_suggestions.from_user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE transfer_suggestions ts
SET from_user_account_id = l.survivor_id
FROM losers l
WHERE ts.from_user_account_id = l.loser_id;

-- 5.6 transfer_suggestions.to_user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE transfer_suggestions ts
SET to_user_account_id = l.survivor_id
FROM losers l
WHERE ts.to_user_account_id = l.loser_id;

-- 5.7 holdings.user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE holdings h
SET user_account_id = l.survivor_id
FROM losers l
WHERE h.user_account_id = l.loser_id;

-- 5.8 investment_transactions.user_account_id (ON DELETE SET NULL — re-pointing anyway)
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE investment_transactions it
SET user_account_id = l.survivor_id
FROM losers l
WHERE it.user_account_id = l.loser_id;

-- =============================================================================
-- Step 6: Delete loser rows
-- =============================================================================

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
DELETE FROM user_accounts ua USING losers l WHERE ua.id = l.loser_id;

-- =============================================================================
-- Step 7: Swap indexes — drop old non-unique, add NULLS NOT DISTINCT unique
-- =============================================================================

DROP INDEX IF EXISTS idx_user_accounts_inst_mask_blind;

CREATE UNIQUE INDEX IF NOT EXISTS unique_user_accounts_inst_mask_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx)
    NULLS NOT DISTINCT
    WHERE institution_id_blind_idx IS NOT NULL;

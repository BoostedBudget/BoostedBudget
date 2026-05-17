-- Backfill transactions.category from personal_finance_category for rows where Plaid
-- stopped sending the legacy category field (all new transactions post-PFC rollout).
-- Only writes to `category`; `original_category` is intentionally left NULL to faithfully
-- reflect that Plaid never sent a legacy category for these rows.
-- Issue: Plaid PFC backfill — 0/42 staging transactions have category populated.
--
-- Idempotent: only touches rows where category IS NULL AND personal_finance_category IS NOT NULL.
-- Chunked in batches of 1000 with SKIP LOCKED + pg_sleep(0.1) to avoid hot-table locks.

-- Helper: SCREAMING_SNAKE_CASE → "Title Case With Spaces"
-- e.g. 'FAST_FOOD' → 'Fast Food', 'FOOD_AND_DRINK' → 'Food And Drink'
CREATE OR REPLACE FUNCTION pfc_enum_to_title_case(val TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
STRICT
AS $$
    SELECT initcap(replace(val, '_', ' '))
$$;

-- Backfill category in chunks of 1000 rows.
DO $$
DECLARE
    rows_updated INT;
    primary_val  TEXT;
    detailed_val TEXT;
    primary_stripped TEXT;
BEGIN
    LOOP
        WITH batch AS (
            SELECT id,
                   personal_finance_category->>'primary'  AS pfc_primary,
                   personal_finance_category->>'detailed' AS pfc_detailed
            FROM   transactions
            WHERE  category IS NULL
              AND  personal_finance_category IS NOT NULL
            LIMIT  1000
            FOR UPDATE SKIP LOCKED
        ),
        computed AS (
            SELECT
                b.id,
                -- Primary display: title-case the primary enum directly
                pfc_enum_to_title_case(b.pfc_primary) AS primary_display,
                -- Detailed display: strip "PRIMARY_" prefix first, then title-case
                CASE
                    WHEN b.pfc_detailed IS NOT NULL
                         AND b.pfc_detailed <> ''
                         AND b.pfc_primary IS NOT NULL
                         AND b.pfc_primary <> ''
                    THEN pfc_enum_to_title_case(
                             CASE
                                 WHEN b.pfc_detailed LIKE (b.pfc_primary || '\_%') ESCAPE '\'
                                 THEN substring(b.pfc_detailed FROM length(b.pfc_primary) + 2)
                                 ELSE b.pfc_detailed
                             END
                         )
                    ELSE NULL
                END AS detailed_display
            FROM batch b
            WHERE b.pfc_primary IS NOT NULL
              AND b.pfc_primary <> ''
        )
        UPDATE transactions t
        SET    category = CASE
                   WHEN c.detailed_display IS NOT NULL
                   THEN to_jsonb(ARRAY[c.primary_display, c.detailed_display])
                   ELSE to_jsonb(ARRAY[c.primary_display])
               END
        FROM   computed c
        WHERE  t.id = c.id;

        GET DIAGNOSTICS rows_updated = ROW_COUNT;
        EXIT WHEN rows_updated = 0;
        PERFORM pg_sleep(0.1);
    END LOOP;
END $$;

COMMENT ON FUNCTION pfc_enum_to_title_case(TEXT) IS
    'Converts a Plaid PFC SCREAMING_SNAKE_CASE enum value to Title Case display string. '
    'Used by migration 000064 to backfill transactions.category from personal_finance_category.';

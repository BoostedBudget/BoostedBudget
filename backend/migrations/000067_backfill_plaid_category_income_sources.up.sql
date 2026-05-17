-- Issue #1089: Backfill income_sources JSONB to replace legacy "plaid_category" type with "pfc_primary"
--
-- "plaid_category" was retired in #1073. Backend validation rejects new writes;
-- this migration rewrites any surviving rows so the stored data matches the canonical type.
-- Idempotency guard: WHERE clause is a no-op on rows that don't contain the old token.

UPDATE budgets
SET income_sources = (
    SELECT jsonb_agg(
        CASE
            WHEN element->>'type' = 'plaid_category'
            THEN jsonb_set(element, '{type}', '"pfc_primary"')
            ELSE element
        END
    )
    FROM jsonb_array_elements(income_sources) AS element
)
WHERE income_sources::text LIKE '%plaid_category%';

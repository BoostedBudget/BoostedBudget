-- Rollback migration for balance_snapshots table
-- Issue: #353 - Balance Tracking Dashboard
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH (#1569 round 12 task 4). balance_snapshots is the
-- only record of what an account's balance was on a past day -- Plaid does not backfill it -- and
-- this DROP TABLE has been unconditional since day one. That was fine before prod go-live (there
-- was nothing to lose); it stops being fine the moment a real user has a snapshot. Same mechanism
-- as 000150/000151/000180's refusal guards: fail loudly rather than silently destroy.
--
-- Conditioned on rows rather than unconditional for the reason those guards give: the rollback
-- suite runs `down -all` against a clean_database, so this is silent there and loud exactly where
-- it matters. Dropping an EMPTY table discards nothing.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM balance_snapshots;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000016: balance_snapshots holds % row(s), and this rollback '
            'DROPs the table. That is the only record of what an account''s balance was on a past '
            'day; Plaid does not backfill it and nothing reconstructs it. Export it, or empty it '
            'deliberately, before retrying.', n;
    END IF;
END $$;

-- Drop table with CASCADE to remove all constraints and dependencies
DROP TABLE IF EXISTS balance_snapshots CASCADE;

-- Indexes will be automatically dropped with the table

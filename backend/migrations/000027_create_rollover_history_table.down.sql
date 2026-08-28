-- Rollback: Drop rollover_history table
-- Issue: #36 - Rule Executors
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH (#1569 round 12 task 4). rollover_history is the
-- only record of budget rollovers between periods. Same mechanism as
-- 000150/000151/000096/000160/000163/000180: REFUSE while rows exist rather than silently destroy.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM rollover_history;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000027: rollover_history holds % row(s), and this rollback '
            'DROPs the table. It is the only record of budget rollovers between periods; nothing '
            'reconstructs it. Export it, or empty it deliberately, before retrying.', n;
    END IF;
END $$;

DROP TABLE IF EXISTS rollover_history CASCADE;

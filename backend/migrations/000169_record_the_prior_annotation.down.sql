-- Rollback for 000169 (#1569 Task 6 / #1579).
--
-- ⚠ THIS DESTROYS THE ONLY COPY OF EVERY OVERWRITTEN ANNOTATION. The prior category override and
-- the pre-existing reviewed_at exist nowhere else — the UPDATE that wrote each row is what removed
-- them from `transactions`.
--
-- ⚠⚠ #1569 round 12 task 4 (Codex-cited example): "take a copy before rolling back if that
-- history matters" was a comment, not a control. Same mechanism as
-- 000150/000151/000096/000160/000163/000027/000020/000097/000180: REFUSE while rows exist. Note
-- 000170/000172/000173 (later migrations against this same table) already refuse via their own
-- CHECK-constraint narrowing failing on 'settlement'/'erasure'/'tags' rows -- this guard is the
-- backstop for plain 'category'/'review' rows, which those three do not see.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM transaction_annotation_history;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000169: transaction_annotation_history holds % row(s), and '
            'this rollback DROPs the table. It is the only copy of every prior category override '
            'and pre-existing reviewed_at that a user or the system has since overwritten; nothing '
            'reconstructs it. Export it, or empty it deliberately, before retrying.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_transaction_annotation_history_transaction;
DROP INDEX IF EXISTS idx_transaction_annotation_history_user_changed;
DROP TABLE IF EXISTS transaction_annotation_history;

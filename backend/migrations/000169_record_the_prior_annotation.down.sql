-- Rollback for 000169 (#1569 Task 6 / #1579).
--
-- ⚠ THIS DESTROYS THE ONLY COPY OF EVERY OVERWRITTEN ANNOTATION. The prior category override and
-- the pre-existing reviewed_at exist nowhere else — the UPDATE that wrote each row is what removed
-- them from `transactions`. Take a copy before rolling back if that history matters.

DROP INDEX IF EXISTS idx_transaction_annotation_history_transaction;
DROP INDEX IF EXISTS idx_transaction_annotation_history_user_changed;
DROP TABLE IF EXISTS transaction_annotation_history;

-- #1625 Task 12: stop a progress_basis round-trip from double-counting the ledger.
--
-- THE DEFECT
--   A goal on the contributions basis with a ledger totalling 100 has current_amount = 100.
--   Switching to manual called ArchiveByGoal, which only cleared managed_by_reconciliation --
--   the rows stayed live and confirmed. Switching BACK ran handleBasisSwitch's seed, inserting a
--   synthetic confirmed contribution equal to the prior current_amount. SumConfirmedByGoal then
--   summed the original 100 PLUS the synthetic 100 and reported 200. The row and the response
--   agreed; the derived value itself was wrong.
--
-- WHY A NEW COLUMN
--   Clearing managed_by_reconciliation made reconciler-created rows INDISTINGUISHABLE from rows
--   the user entered by hand, because user rows already carry false. There was no surviving
--   signal for "belongs to a closed contributions period", so one has to be added.
--
-- ⚠ NO BACKFILL, DELIBERATELY. Rows already flattened by the old ArchiveByGoal cannot be told
--   apart from legitimate manual confirmed contributions. Guessing would silently destroy real
--   balances on goals that never round-tripped. Existing goals keep whatever current_amount they
--   have; only future basis switches are correct.
--
-- No index: every read that filters on this column is already scoped by savings_goal_id, which
-- is indexed, and the per-goal row count is small.
ALTER TABLE savings_goal_contributions
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

COMMENT ON COLUMN savings_goal_contributions.archived_at IS
    'Set when a goal leaves (or re-enters) the contributions progress_basis. An archived row is a historical record of a CLOSED contributions period: it is retained and still readable, but excluded from SumConfirmedByGoal/SumConfirmedByGoalInPeriod so it cannot double-count against the synthetic seed handleBasisSwitch inserts. NULL means the row counts toward the goal''s current progress. #1625.';

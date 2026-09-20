-- #1693: give a permanently non-converging RevenueCat operation a terminal state.
--
-- Before this, stage='pending' was the only failure state: ProcessOperation rewrote 'pending' on
-- every failure, FinishOperation only bumped attempts, and RevenueCatRepairWorker re-enqueued every
-- pending row every minute — so an operation the provider never reflects re-applied its revoke/grant
-- side effects forever. ProcessOperation now writes 'abandoned' once an operation reaches its
-- attempt cap, and every stage='pending' read (PendingOperations, the Read/Save projection gates,
-- the admin health and job-health counts) excludes it by construction.
--
-- Additive: the CHECK is only WIDENED — no value is removed and no row is rewritten, so this
-- migration is safe to re-run and changes no data. NOT VALID + VALIDATE keeps the existing-row scan
-- out of ACCESS EXCLUSIVE (the 000124-000126 pattern); the constraint still rejects a bad NEW row
-- from the instant it is added, so there is no enforcement window.
ALTER TABLE revenuecat_operations DROP CONSTRAINT IF EXISTS revenuecat_operations_stage_check;
ALTER TABLE revenuecat_operations
    ADD CONSTRAINT revenuecat_operations_stage_check
    CHECK (stage IN ('pending', 'applied', 'superseded', 'abandoned')) NOT VALID;
ALTER TABLE revenuecat_operations VALIDATE CONSTRAINT revenuecat_operations_stage_check;

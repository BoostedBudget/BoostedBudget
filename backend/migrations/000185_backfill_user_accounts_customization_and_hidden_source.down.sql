-- Rollback: 000185_backfill_user_accounts_customization_and_hidden_source
-- Issue: #1609 (Item 3, part 1 of 3)
--
-- WHAT THIS UNDOES, AND WHAT IT DELIBERATELY DOES NOT
--
-- Reversible: Step 3's hidden_source column and its CHECK constraint. Dropping the column drops
-- the Step 4 classification with it, so the "why is this hidden" state is fully removed.
--
-- NOT reversed: the Step 1 (is_hidden) and Step 2 (nickname / custom_color / custom_icon /
-- custom_emoji / display_order) lifts. This is a choice, not an omission.
--
--   * They are not invertible. The up migration writes into columns that were NULL (or FALSE) and
--     records nothing about which rows it touched, so there is no way to distinguish a value it
--     lifted from one the user set through the API a minute later. "Undoing" it could only be
--     spelled as "NULL out every chain-level customization", which would destroy user data that
--     this migration never wrote.
--   * They are not harmful when the code is rolled back. Before task 3b, the read path takes the
--     accounts columns and ignores user_accounts entirely, so a lifted value is inert — it is
--     written but not read. Rolling the code back therefore restores the old behaviour with or
--     without this data.
--   * BoostedBudget retains financial history by default
--     (docs/decisions/2026-08-24-financial-history-retained-by-default.md). A rollback is not a
--     user asking for their customization to be destroyed, so a down migration must not destroy
--     it.
--
-- A down migration is required to leave the SCHEMA identical to the pre-up state, which this does.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_accounts_valid_hidden_source'
    ) THEN
        ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_valid_hidden_source;
    END IF;
END $$;

ALTER TABLE user_accounts DROP COLUMN IF EXISTS hidden_source;

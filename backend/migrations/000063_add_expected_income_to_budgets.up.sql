-- Issue #1048: Add expected_income column to budgets table.
-- Stores the user-declared income target per budget period.
-- Additive: existing amount column kept as back-compat mirror; backfilled here.

-- Add expected_income column (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'expected_income'
    ) THEN
        ALTER TABLE budgets
            ADD COLUMN expected_income DECIMAL(15,2) NOT NULL DEFAULT 0
            CHECK (expected_income >= 0);
    END IF;
END $$;

-- Backfill existing rows from amount where expected_income has not yet been set.
UPDATE budgets SET expected_income = amount WHERE expected_income = 0;

COMMENT ON COLUMN budgets.expected_income IS 'User-declared income target for this budget period (envelope budgeting). Issue #1048.';

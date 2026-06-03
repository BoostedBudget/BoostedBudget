-- Update column comment to reflect PFCv2 sign convention (positive=outflow, negative=inflow).
-- This is a no-op migration: comment-only change, fully idempotent.
COMMENT ON COLUMN transactions.amount IS
    'Transaction amount under Plaid PFCv2 convention. Positive = outflow (spending/debit). Negative = inflow (refund/income/credit).';

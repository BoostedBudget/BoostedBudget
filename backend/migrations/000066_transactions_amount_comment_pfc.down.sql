-- Restore the pre-PFCv2 column comment.
COMMENT ON COLUMN transactions.amount IS
    'Transaction amount. Positive for credits, negative for debits (per Plaid convention)';

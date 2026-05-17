-- #964: Drop old plaintext unique constraints that have been replaced by blind index constraints
-- Run ONLY after all data has been encrypted (encrypt-migrate tool has completed) AND
-- after migration 000054 has been applied (blind index constraints are active).

-- Drop the plaintext unique constraint on transactions
-- (replaced by unique_source_transaction_blind from migration 000054)
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS unique_source_transaction;

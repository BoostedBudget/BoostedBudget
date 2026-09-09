-- Reverses 000192.
--
-- Dropping the column loses only the optimistic-concurrency token, which is derived bookkeeping
-- rather than user or financial data: no read reports it to a user, and every rule's meaning
-- (name, conditions, target) is untouched. Rolling back therefore restores the pre-000192
-- `updated_at`-keyed guard, with the false-conflict defect that key implies — a behavioural
-- regression, not data loss, so this needs no refusal of the 000180 kind.
ALTER TABLE categorization_rules
    DROP COLUMN IF EXISTS version;

-- #964: Restore old plaintext unique constraints

ALTER TABLE transactions
    ADD CONSTRAINT unique_source_transaction UNIQUE (source, external_transaction_id, user_id);

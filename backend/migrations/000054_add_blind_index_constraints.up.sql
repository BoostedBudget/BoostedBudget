-- #964: Add unique constraints on blind index columns
-- These coexist with old plaintext constraints during the migration window.
-- Must be run AFTER the encrypt-migrate data backfill tool populates blind index columns.

CREATE UNIQUE INDEX IF NOT EXISTS unique_source_transaction_blind
    ON transactions (source, external_transaction_id_blind_idx, user_id);

CREATE UNIQUE INDEX IF NOT EXISTS unique_linked_items_item_id_blind
    ON linked_items (item_id_blind_idx)
    WHERE item_id_blind_idx IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS unique_accounts_plaid_id_blind
    ON accounts (plaid_account_id_blind_idx)
    WHERE plaid_account_id_blind_idx IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS unique_users_email_blind
    ON users (email_blind_idx)
    WHERE email_blind_idx IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS unique_users_clerk_id_blind
    ON users (clerk_user_id_blind_idx)
    WHERE clerk_user_id_blind_idx IS NOT NULL;

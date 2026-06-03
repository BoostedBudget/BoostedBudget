-- #964: Drop unique constraints on blind index columns

DROP INDEX IF EXISTS unique_users_clerk_id_blind;
DROP INDEX IF EXISTS unique_users_email_blind;
DROP INDEX IF EXISTS unique_accounts_plaid_id_blind;
DROP INDEX IF EXISTS unique_linked_items_item_id_blind;
DROP INDEX IF EXISTS unique_source_transaction_blind;

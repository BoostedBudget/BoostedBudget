-- Rollback: Drop linked_items table
-- WARNING: This will delete ALL Plaid item connections and encrypted access tokens
-- Only use in development or with full backup

DROP TABLE IF EXISTS linked_items CASCADE;

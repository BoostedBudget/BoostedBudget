-- Migration: Backfill user_account_id on accounts and transactions
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Link existing accounts and transactions to their stable user_account
--
-- NOTE: This migration is a NO-OP because there is no production data yet.
-- user_account_id will be set at runtime when:
-- 1. Accounts are created (via AccountService.LinkAccounts)
-- 2. Transactions are synced (via TransactionSyncService)
--
-- If we had existing data, this would:
-- 1. Set user_account_id on accounts by matching (user_id, institution_id, mask)
-- 2. Set user_account_id on transactions through their linked account

-- =============================================================================
-- NO-OP Migration - user_account_id will be set at runtime
-- =============================================================================

-- Intentionally empty - no backfill needed without existing data

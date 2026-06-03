-- #1001: Fix encryption constraints to prevent duplicate accounts from pre-encryption rows.
--
-- Problem: When encrypt-migrate is not run before deploying encrypted app code, pre-existing
-- users have NULL blind indexes and are invisible to blind-index lookups. When they log in,
-- the registration service creates a second (duplicate) account.
--
-- Fixes:
-- 1. Drop the stale plaintext UNIQUE constraint on users.email. Post-encryption this column
--    stores ENC$ ciphertext, so comparing raw values is meaningless — it can't catch
--    plaintext-vs-ciphertext duplicates.
-- 2. Convert blind index UNIQUE constraints from partial (WHERE IS NOT NULL) to full.
--    This ensures rows with NULL blind indexes cannot silently bypass uniqueness.
--
-- Must be run AFTER all rows have populated blind indexes (i.e., encrypt-migrate has run).

-- 1. Drop the old plaintext unique constraint on users.email
--    (replaced by unique_users_email_blind which compares normalized HMAC-SHA256 blind indexes)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;

-- 2. Convert partial blind index unique indexes to full unique indexes.
--    The WHERE IS NOT NULL partial condition allowed NULL rows to bypass uniqueness,
--    enabling duplicate accounts when encrypt-migrate was not run.
DROP INDEX IF EXISTS unique_users_email_blind;
CREATE UNIQUE INDEX unique_users_email_blind ON users (email_blind_idx);

DROP INDEX IF EXISTS unique_users_clerk_id_blind;
CREATE UNIQUE INDEX unique_users_clerk_id_blind ON users (clerk_user_id_blind_idx);

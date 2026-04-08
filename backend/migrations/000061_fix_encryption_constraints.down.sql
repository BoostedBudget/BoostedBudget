-- #1001: Reverse migration 000060_fix_encryption_constraints

-- Restore the old partial blind index unique constraints (WHERE IS NOT NULL)
DROP INDEX IF EXISTS unique_users_email_blind;
CREATE UNIQUE INDEX unique_users_email_blind
    ON users (email_blind_idx)
    WHERE email_blind_idx IS NOT NULL;

DROP INDEX IF EXISTS unique_users_clerk_id_blind;
CREATE UNIQUE INDEX unique_users_clerk_id_blind
    ON users (clerk_user_id_blind_idx)
    WHERE clerk_user_id_blind_idx IS NOT NULL;

-- Restore the plaintext email unique constraint
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);

-- #1365: Phase 1 — household_invites table.
--
-- Emails are encrypted at rest with AES-GCM (random nonce → non-deterministic ciphertext).
-- A unique/equality constraint on the ciphertext column cannot work, because two encryptions
-- of the same email produce different ciphertexts. Instead we follow the users.email pattern:
--
--   invitee_email          TEXT NOT NULL — AES-GCM ciphertext, written by the repo (Phase 3).
--   invitee_email_blind_idx TEXT NOT NULL — deterministic HMAC-SHA256 of the normalised email,
--                                           computed via crypto.FieldEncryptor.BlindIndex().
--                                           Carries the uniqueness constraint and lookup index.
--
-- token_hash: SHA-256 of the one-time invite token sent to the invitee. The plaintext token
-- is never stored. UNIQUE (token_hash) for O(1) accept/decline lookups.
--
-- accepted_at: durable timestamp of acceptance. Even if accepted_user_id is later SET NULL
-- by a GDPR user deletion, the acceptance timestamp survives for audit purposes.

-- =============================================================================
-- Step 1: Create household_invites table
-- =============================================================================

CREATE TABLE IF NOT EXISTS household_invites (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id            UUID        NOT NULL,
    inviter_user_id         UUID        NOT NULL,
    invitee_email           TEXT        NOT NULL,
    invitee_email_blind_idx TEXT        NOT NULL,
    token_hash              TEXT        NOT NULL,
    status                  TEXT        NOT NULL    DEFAULT 'pending'
                                                    CHECK (status IN (
                                                        'pending', 'accepted', 'declined',
                                                        'revoked', 'expired'
                                                    )),
    expires_at              TIMESTAMPTZ NOT NULL,
    accepted_user_id        UUID,
    accepted_at             TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL    DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL    DEFAULT now(),

    -- If status is 'accepted', accepted_at must be set (survives GDPR accepted_user_id deletion).
    CONSTRAINT chk_household_invites_accepted_at CHECK (
        status <> 'accepted' OR accepted_at IS NOT NULL
    )
);

COMMENT ON TABLE household_invites IS
    'One row per invite sent to a prospective household member. '
    'invitee_email stores AES-GCM ciphertext; invitee_email_blind_idx stores the HMAC for '
    'equality/uniqueness lookups — mirrors the users.email encryption pattern. '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN household_invites.invitee_email IS
    'AES-GCM ciphertext of the invitee''s normalised email address. '
    'Non-deterministic — do not use for lookups; use invitee_email_blind_idx.';

COMMENT ON COLUMN household_invites.invitee_email_blind_idx IS
    'Deterministic HMAC-SHA256 blind index of the normalised email. '
    'Used for equality lookups and the pending-invite uniqueness constraint.';

COMMENT ON COLUMN household_invites.token_hash IS
    'SHA-256 hash of the one-time invite token. Plaintext token is never stored.';

COMMENT ON COLUMN household_invites.accepted_at IS
    'Durable timestamp of acceptance. Survives GDPR deletion of accepted_user_id (SET NULL).';

-- =============================================================================
-- Step 2: Foreign keys
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE household_invites
        ADD CONSTRAINT fk_household_invites_household_id
        FOREIGN KEY (household_id) REFERENCES households(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_invites VALIDATE CONSTRAINT fk_household_invites_household_id;

DO $$ BEGIN
    ALTER TABLE household_invites
        ADD CONSTRAINT fk_household_invites_inviter_user_id
        FOREIGN KEY (inviter_user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_invites VALIDATE CONSTRAINT fk_household_invites_inviter_user_id;

DO $$ BEGIN
    ALTER TABLE household_invites
        ADD CONSTRAINT fk_household_invites_accepted_user_id
        FOREIGN KEY (accepted_user_id) REFERENCES users(id)
        ON DELETE SET NULL
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_invites VALIDATE CONSTRAINT fk_household_invites_accepted_user_id;

-- =============================================================================
-- Step 3: Indexes
-- =============================================================================

-- One pending invite per (household, invitee_email).
CREATE UNIQUE INDEX IF NOT EXISTS uq_household_invites_pending_email
    ON household_invites (household_id, invitee_email_blind_idx)
    WHERE status = 'pending';

-- Fast token lookup for accept/decline flows.
CREATE UNIQUE INDEX IF NOT EXISTS uq_household_invites_token_hash
    ON household_invites (token_hash);

-- Lookup pending invites by email (e.g. "do I have a pending invite?" at signup).
CREATE INDEX IF NOT EXISTS idx_household_invites_pending_email_blind_idx
    ON household_invites (invitee_email_blind_idx)
    WHERE status = 'pending';

-- Expiry sweep: find all pending invites past their expires_at (Phase 3 cron).
CREATE INDEX IF NOT EXISTS idx_household_invites_pending_expiry
    ON household_invites (expires_at)
    WHERE status = 'pending';

-- =============================================================================
-- Step 4: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_household_invites_updated_at'
    ) THEN
        CREATE TRIGGER update_household_invites_updated_at
            BEFORE UPDATE ON household_invites
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

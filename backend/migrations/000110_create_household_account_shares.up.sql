-- #1365: Phase 1 — household_account_shares table.
--
-- A grant that makes a member's bank account visible in the household aggregate.
-- Keyed to user_accounts (stable across Plaid relinks), not accounts.
--
-- Design decisions:
--   - owner_user_id is NOT stored here. The account owner is user_accounts.user_id,
--     which is the authoritative source. A denormalized owner_user_id column could
--     disagree with it (cross-wire defect). Phase 5 fan-out joins user_accounts
--     to derive ownership, not this table.
--   - physical_account_blind_idx: deterministic HMAC of the physical account identity
--     (institution_id_blind_idx + mask_blind_idx). Used by Phase 5 to deduplicate
--     joint accounts (two members linking the same real-world bank account). Nullable
--     because not all accounts have a deterministic fingerprint (e.g. mask-less).
--   - is_hidden (on accounts and user_accounts) is NOT touched. That flag governs the
--     owner's own personal view. This table governs household aggregate inclusion.
--
-- Lifecycle: shared_at is set at grant creation (NOT NULL); revoked_at is set when
-- a share is withdrawn. The partial unique index ensures at most one active grant
-- per (household, user_account). After revocation a new share row may be inserted.

-- =============================================================================
-- Step 1: Create household_account_shares table
-- =============================================================================

CREATE TABLE IF NOT EXISTS household_account_shares (
    id                          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id                UUID            NOT NULL,
    user_account_id             UUID            NOT NULL,
    physical_account_blind_idx  TEXT,
    visibility                  TEXT            NOT NULL    DEFAULT 'shared'
                                                            CHECK (visibility IN ('shared', 'hidden')),
    shared_at                   TIMESTAMPTZ     NOT NULL    DEFAULT now(),
    revoked_at                  TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ     NOT NULL    DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL    DEFAULT now(),

    -- revoked_at, when set, must not be before shared_at.
    CONSTRAINT chk_household_account_shares_revoked_at CHECK (
        revoked_at IS NULL OR revoked_at >= shared_at
    )
);

COMMENT ON TABLE household_account_shares IS
    'Grant table for household account visibility. One active row per '
    '(household_id, user_account_id). Keyed to user_accounts (survives Plaid relinks). '
    'owner_user_id is intentionally omitted — derived from user_accounts.user_id at query time. '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN household_account_shares.user_account_id IS
    'References user_accounts(id) — the stable abstraction that survives Plaid re-links. '
    'ON DELETE CASCADE: if the user_account is removed, the share grant is removed too.';

COMMENT ON COLUMN household_account_shares.physical_account_blind_idx IS
    'Deterministic HMAC of the physical account identity (institution_id_blind_idx || mask_blind_idx). '
    'Used by Phase 5 to deduplicate joint accounts in aggregate queries. '
    'NULL for accounts without a deterministic fingerprint (e.g. mask-less).';

COMMENT ON COLUMN household_account_shares.visibility IS
    'shared: included in the household aggregate. '
    'hidden: member chose to exclude this account from the shared view.';

COMMENT ON COLUMN household_account_shares.shared_at IS
    'Timestamp when this grant was created (NOT NULL). '
    'revoked_at (when set) must be >= shared_at.';

-- =============================================================================
-- Step 2: Foreign keys
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE household_account_shares
        ADD CONSTRAINT fk_household_account_shares_household_id
        FOREIGN KEY (household_id) REFERENCES households(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_account_shares VALIDATE CONSTRAINT fk_household_account_shares_household_id;

DO $$ BEGIN
    ALTER TABLE household_account_shares
        ADD CONSTRAINT fk_household_account_shares_user_account_id
        FOREIGN KEY (user_account_id) REFERENCES user_accounts(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_account_shares VALIDATE CONSTRAINT fk_household_account_shares_user_account_id;

-- =============================================================================
-- Step 3: Indexes
-- =============================================================================

-- One active grant per (household, user_account). Allows a new share row after revocation.
CREATE UNIQUE INDEX IF NOT EXISTS uq_household_account_shares_active
    ON household_account_shares (household_id, user_account_id)
    WHERE revoked_at IS NULL;

-- Household aggregate query: all active shared accounts for a household.
CREATE INDEX IF NOT EXISTS idx_household_account_shares_household_visibility
    ON household_account_shares (household_id, visibility);

-- Reverse lookup: all households an account is shared into (used on account deletion).
CREATE INDEX IF NOT EXISTS idx_household_account_shares_user_account
    ON household_account_shares (user_account_id);

-- Phase 5 active-visible aggregation path: partial covering index.
CREATE INDEX IF NOT EXISTS idx_household_account_shares_active_visible
    ON household_account_shares (household_id, user_account_id)
    WHERE revoked_at IS NULL AND visibility = 'shared';

-- =============================================================================
-- Step 4: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_household_account_shares_updated_at'
    ) THEN
        CREATE TRIGGER update_household_account_shares_updated_at
            BEFORE UPDATE ON household_account_shares
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

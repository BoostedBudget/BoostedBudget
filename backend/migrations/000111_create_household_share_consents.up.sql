-- #1365: Phase 1 — household_share_consents table.
--
-- Append-only audit log capturing the moment a user explicitly consented to
-- household data sharing. Written once per consent event; rows are never updated.
-- snapshot (JSONB) records which accounts were included (and their visibility) at
-- the moment of consent — provides a durable audit trail for GDPR purposes.
--
-- Intentionally has NO updated_at column and NO trigger (append-only).

-- =============================================================================
-- Step 1: Create household_share_consents table
-- =============================================================================

CREATE TABLE IF NOT EXISTS household_share_consents (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id    UUID        NOT NULL,
    user_id         UUID        NOT NULL,
    consent_version TEXT        NOT NULL,
    consented_at    TIMESTAMPTZ NOT NULL    DEFAULT now(),
    snapshot        JSONB       NOT NULL    DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL    DEFAULT now()
);

COMMENT ON TABLE household_share_consents IS
    'Append-only audit log of explicit household data-sharing consent events. '
    'One row per consent action (join, re-consent on policy change). '
    'Never updated — no updated_at column or trigger. '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN household_share_consents.consent_version IS
    'Version identifier of the consent policy presented to the user '
    '(e.g. "v1", "v2026-01"). Allows auditing which policy version was accepted.';

COMMENT ON COLUMN household_share_consents.snapshot IS
    'JSONB snapshot of the accounts (and their visibility) included at consent time. '
    'Provides a durable audit trail even if accounts are later hidden/removed. '
    'NOT NULL DEFAULT ''{}'' — empty object when no accounts were shared yet.';

-- =============================================================================
-- Step 2: Foreign keys
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE household_share_consents
        ADD CONSTRAINT fk_household_share_consents_household_id
        FOREIGN KEY (household_id) REFERENCES households(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_share_consents VALIDATE CONSTRAINT fk_household_share_consents_household_id;

DO $$ BEGIN
    ALTER TABLE household_share_consents
        ADD CONSTRAINT fk_household_share_consents_user_id
        FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_share_consents VALIDATE CONSTRAINT fk_household_share_consents_user_id;

-- =============================================================================
-- Step 3: Index — audit lookup: all consents for a (household, user) pair
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_household_share_consents_household_user
    ON household_share_consents (household_id, user_id);

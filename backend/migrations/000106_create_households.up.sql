-- #1365: Phase 1 — Multi-Member Household Accounts schema foundation.
--
-- households: one row per paying household owner. A user can own at most one
-- household (UNIQUE owner_user_id). The subscription tier is NOT stored here —
-- it is always derived from the owner's subscription_entitlements row (Phase 2).
--
-- name: user-visible display name for the household (e.g. "The Smith Family").
--       DB CHECK enforces the same 1–255-char rule as domain.Household.Validate().

-- =============================================================================
-- Step 1: Create households table
-- =============================================================================

CREATE TABLE IF NOT EXISTS households (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id   UUID        NOT NULL,
    name            TEXT        NOT NULL    DEFAULT 'My Household'
                                CHECK (char_length(btrim(name)) BETWEEN 1 AND 255),
    created_at      TIMESTAMPTZ NOT NULL    DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL    DEFAULT now()
);

COMMENT ON TABLE households IS
    'One row per household owner. A user may own at most one household '
    '(UNIQUE owner_user_id). Subscription tier is derived from subscription_entitlements, '
    'not stored here. Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN households.owner_user_id IS
    'The paying subscriber who created and administers this household. '
    'Cascaded on user deletion — dissolves the household.';

COMMENT ON COLUMN households.name IS
    'User-visible household display name (e.g. "The Smith Family"). '
    '1–255 non-whitespace characters.';

-- =============================================================================
-- Step 2: Unique constraint — one household per owner
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE households
        ADD CONSTRAINT uq_households_owner_user_id
        UNIQUE (owner_user_id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- =============================================================================
-- Step 3: Foreign key — owner_user_id → users(id) ON DELETE CASCADE
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE households
        ADD CONSTRAINT fk_households_owner_user_id
        FOREIGN KEY (owner_user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE households VALIDATE CONSTRAINT fk_households_owner_user_id;

-- =============================================================================
-- Step 4: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_households_updated_at'
    ) THEN
        CREATE TRIGGER update_households_updated_at
            BEFORE UPDATE ON households
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- #1366: Phase 2 — enforce the two invariants Phase 1 deferred (000108, lines 85-95):
--   1. One active (effective_until IS NULL) entitlement per user.
--   2. payer_user_id must equal the household's owner_user_id (composite FK).
--
-- Both tables are empty in production (Phase 1 wrote zero rows), so the ALTER TABLE
-- locks below are effectively free. squawk-ignore is safe here for that reason only —
-- do not reuse this pattern once these tables carry data.

-- =============================================================================
-- Step 1: households needs a composite UNIQUE(id, owner_user_id) to serve as the
-- FK target below. id is already the PK; owner_user_id is already UNIQUE alone.
-- This composite constraint is structurally redundant but required by Postgres —
-- an FK target must be backed by a unique constraint, not just a unique index.
-- =============================================================================

-- squawk-ignore disallowed-unique-constraint
DO $$ BEGIN
    ALTER TABLE households
        ADD CONSTRAINT uq_households_id_owner_user_id
        UNIQUE (id, owner_user_id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- =============================================================================
-- Step 2: composite FK — payer_user_id must be the owner of household_id.
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE subscription_entitlements
        ADD CONSTRAINT fk_subscription_entitlements_payer_is_owner
        FOREIGN KEY (household_id, payer_user_id) REFERENCES households(id, owner_user_id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE subscription_entitlements VALIDATE CONSTRAINT fk_subscription_entitlements_payer_is_owner;

-- =============================================================================
-- Step 3: one active entitlement per user (effective_until IS NULL = open epoch).
-- require-concurrent-index-creation is excluded project-wide (small tables), so a
-- plain CREATE UNIQUE INDEX is fine here.
-- =============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS uq_subscription_entitlements_active_user
    ON subscription_entitlements (user_id)
    WHERE effective_until IS NULL;

COMMENT ON INDEX uq_subscription_entitlements_active_user IS
    'Enforces one open-ended (effective_until IS NULL) entitlement per user. '
    'Part of issue #1366 — Multi-Member Household Accounts Phase 2.';

-- #1365: Phase 1 — household_members table.
--
-- One row per (household, user) per lifecycle epoch. Removed rows are retained
-- for history and rejoin tracking; removed_at records when removal occurred.
--
-- Key invariants (enforced by partial unique indexes):
--   1. A user is in at most one active household at a time:
--      UNIQUE (user_id) WHERE status='active'
--   2. A household has exactly one active owner:
--      UNIQUE (household_id) WHERE role='owner' AND status='active'
--
-- The mandatory owner row (role='owner', status='active', user_id=households.owner_user_id)
-- is inserted in the same transaction as the households row by the service layer (Phase 3).
-- Because the owner row is an active member, it counts against invariant #1 — preventing
-- the owner from simultaneously joining another household.
--
-- prior_subscription_tier / prior_subscription_status: snapshot of the member's own
-- subscription at join time, used by Phase 7 offboarding to restore their tier on leave.

-- =============================================================================
-- Step 1: Create household_members table
-- =============================================================================

CREATE TABLE IF NOT EXISTS household_members (
    id                          UUID                    PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id                UUID                    NOT NULL,
    user_id                     UUID                    NOT NULL,
    role                        TEXT                    NOT NULL    CHECK (role IN ('owner', 'member')),
    status                      TEXT                    NOT NULL    DEFAULT 'active'
                                                                    CHECK (status IN ('active', 'removed')),
    joined_at                   TIMESTAMPTZ             NOT NULL    DEFAULT now(),
    removed_at                  TIMESTAMPTZ,
    prior_subscription_tier     TEXT,
    prior_subscription_status   TEXT,
    created_at                  TIMESTAMPTZ             NOT NULL    DEFAULT now(),
    updated_at                  TIMESTAMPTZ             NOT NULL    DEFAULT now(),

    -- status/removed_at pairing: active rows must not have removed_at; removed rows must.
    CONSTRAINT chk_household_members_status_removed_at CHECK (
        (status = 'active'  AND removed_at IS NULL) OR
        (status = 'removed' AND removed_at IS NOT NULL)
    )
);

COMMENT ON TABLE household_members IS
    'One row per (household, user) per lifecycle epoch. '
    'Removed rows are retained for history and rejoin tracking. '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN household_members.role IS
    'owner: the paying subscriber who created this household. '
    'member: a user covered under the owner''s plan.';

COMMENT ON COLUMN household_members.status IS
    'active: currently in the household. '
    'removed: left or was removed; removed_at records the timestamp.';

COMMENT ON COLUMN household_members.prior_subscription_tier IS
    'Snapshot of the member''s own subscription_tier at join time. '
    'Used by Phase 7 offboarding to restore their tier on leave/removal.';

COMMENT ON COLUMN household_members.prior_subscription_status IS
    'Snapshot of the member''s own subscription_status at join time. '
    'Used by Phase 7 offboarding to restore their tier on leave/removal.';

-- =============================================================================
-- Step 2: Foreign keys
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE household_members
        ADD CONSTRAINT fk_household_members_household_id
        FOREIGN KEY (household_id) REFERENCES households(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_members VALIDATE CONSTRAINT fk_household_members_household_id;

DO $$ BEGIN
    ALTER TABLE household_members
        ADD CONSTRAINT fk_household_members_user_id
        FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE household_members VALIDATE CONSTRAINT fk_household_members_user_id;

-- =============================================================================
-- Step 3: Partial unique indexes (invariants 1 & 2)
-- =============================================================================

-- Invariant 1: a user is in at most one active household at a time.
CREATE UNIQUE INDEX IF NOT EXISTS uq_household_members_active_user
    ON household_members (user_id)
    WHERE status = 'active';

-- Invariant 2: a household has exactly one active owner row.
CREATE UNIQUE INDEX IF NOT EXISTS uq_household_members_active_owner
    ON household_members (household_id)
    WHERE role = 'owner' AND status = 'active';

-- =============================================================================
-- Step 4: Supporting index
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_household_members_household_status
    ON household_members (household_id, status);

-- =============================================================================
-- Step 5: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_household_members_updated_at'
    ) THEN
        CREATE TRIGGER update_household_members_updated_at
            BEFORE UPDATE ON household_members
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

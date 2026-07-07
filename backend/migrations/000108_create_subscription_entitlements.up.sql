-- #1365: Phase 1 — subscription_entitlements table.
--
-- Decouples a user's effective subscription coverage from their users row, which is
-- overwritten by Paddle webhooks on billing events. Paddle webhooks write
-- users.subscription_tier by user_id — mutating a member's row directly would be
-- clobbered by their own future billing events. This table survives those writes.
--
-- source = 'own_subscription': the user pays for their own plan.
--   payer_user_id and household_id MUST be NULL.
-- source = 'household': the user is covered under a household owner's plan.
--   payer_user_id and household_id MUST be NOT NULL.
--
-- effective_until = NULL means the entitlement is open-ended (currently active epoch).
-- Phase 2 wires this table into GetEffectiveSubscription() and enforces the
-- one-active-entitlement invariant (UNIQUE partial index or overlap guard).

-- =============================================================================
-- Step 1: Create subscription_entitlements table
-- =============================================================================

CREATE TABLE IF NOT EXISTS subscription_entitlements (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL,
    source          TEXT        NOT NULL    CHECK (source IN ('own_subscription', 'household')),
    tier            TEXT        NOT NULL,
    status          TEXT        NOT NULL    CHECK (status IN (
                                                'active', 'inactive', 'trial', 'trialing',
                                                'cancelled', 'past_due'
                                            )),
    payer_user_id   UUID,
    household_id    UUID,
    effective_from  TIMESTAMPTZ NOT NULL    DEFAULT now(),
    effective_until TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL    DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL    DEFAULT now(),

    -- source/household_id/payer_user_id shape constraint.
    -- own_subscription: household coverage columns must be null.
    -- household: both coverage columns must be present.
    CONSTRAINT chk_subscription_entitlements_source_shape CHECK (
        (source = 'own_subscription' AND household_id IS NULL     AND payer_user_id IS NULL) OR
        (source = 'household'        AND household_id IS NOT NULL AND payer_user_id IS NOT NULL)
    ),

    -- effective_until, when set, must be strictly after effective_from.
    CONSTRAINT chk_subscription_entitlements_dates CHECK (
        effective_until IS NULL OR effective_until > effective_from
    )
);

COMMENT ON TABLE subscription_entitlements IS
    'Decouples subscription coverage from the payer''s users row. '
    'Paddle webhooks overwrite users.subscription_tier by user_id; this table is '
    'not touched by webhooks directly. Phase 2 wires it into GetEffectiveSubscription(). '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN subscription_entitlements.source IS
    'own_subscription: user pays for their own plan (payer_user_id and household_id NULL). '
    'household: user is covered under the household owner''s plan '
    '(payer_user_id and household_id NOT NULL).';

COMMENT ON COLUMN subscription_entitlements.effective_until IS
    'NULL = open-ended (current epoch). '
    'Set when the entitlement ends (e.g. member leaves household, owner cancels). '
    'Phase 2 adds a partial unique index on (user_id) WHERE effective_until IS NULL '
    'to enforce one-active-entitlement per user.';

-- =============================================================================
-- Step 2: Foreign keys
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE subscription_entitlements
        ADD CONSTRAINT fk_subscription_entitlements_user_id
        FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE subscription_entitlements VALIDATE CONSTRAINT fk_subscription_entitlements_user_id;

-- ON DELETE CASCADE: if the household owner (payer) is GDPR-deleted, the household cascades
-- away first (via fk_subscription_entitlements_household_id), removing entitlement rows before
-- this FK fires. CASCADE here avoids a logical contradiction: the source='household' CHECK
-- requires payer_user_id IS NOT NULL, so SET NULL would violate the constraint on deletion.
DO $$ BEGIN
    ALTER TABLE subscription_entitlements
        ADD CONSTRAINT fk_subscription_entitlements_payer_user_id
        FOREIGN KEY (payer_user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE subscription_entitlements VALIDATE CONSTRAINT fk_subscription_entitlements_payer_user_id;

DO $$ BEGIN
    ALTER TABLE subscription_entitlements
        ADD CONSTRAINT fk_subscription_entitlements_household_id
        FOREIGN KEY (household_id) REFERENCES households(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE subscription_entitlements VALIDATE CONSTRAINT fk_subscription_entitlements_household_id;

-- =============================================================================
-- Step 3: Indexes
-- =============================================================================

-- Primary lookup: get all entitlements for a user filtered by status.
CREATE INDEX IF NOT EXISTS idx_subscription_entitlements_user_status
    ON subscription_entitlements (user_id, status);

-- Household-scoped lookup: get all member entitlements for a household.
CREATE INDEX IF NOT EXISTS idx_subscription_entitlements_household
    ON subscription_entitlements (household_id)
    WHERE household_id IS NOT NULL;

-- =============================================================================
-- Step 4: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_subscription_entitlements_updated_at'
    ) THEN
        CREATE TRIGGER update_subscription_entitlements_updated_at
            BEFORE UPDATE ON subscription_entitlements
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

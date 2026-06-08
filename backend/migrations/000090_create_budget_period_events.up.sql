-- Issue #972: Savings Transfer Automation — A5 Migration
-- Create budget_period_events marker table for exactly-once budget_period_end event firing.
-- Each row represents a fired event for a specific budget period, preventing duplicate events.

-- =============================================================================
-- Step 1: Create budget_period_events table
-- =============================================================================

CREATE TABLE IF NOT EXISTS budget_period_events (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT        NOT NULL,
    budget_id  UUID        NOT NULL,
    period_start DATE      NOT NULL,
    period_end   DATE      NOT NULL,
    event_type TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =============================================================================
-- Step 2: Unique index — the idempotency guard
-- Prevents duplicate events for the same (user, budget, period, event_type).
-- =============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS idx_budget_period_events_unique
    ON budget_period_events(user_id, budget_id, period_start, period_end, event_type);

-- =============================================================================
-- Step 3: Index for efficient lookups by budget_id (also supports FK cascade)
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_budget_period_events_budget_id
    ON budget_period_events(budget_id);

-- =============================================================================
-- Step 4: FK from budget_id → budgets(id) ON DELETE CASCADE
-- Using NOT VALID + VALIDATE pattern for zero-downtime (matches project convention).
-- ON DELETE CASCADE: deleting a budget cleans up its period event markers.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_budget_period_events_budget'
    ) THEN
        ALTER TABLE budget_period_events
            ADD CONSTRAINT fk_budget_period_events_budget
            FOREIGN KEY (budget_id)
            REFERENCES budgets(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_budget_period_events_budget'
    ) THEN
        ALTER TABLE budget_period_events VALIDATE CONSTRAINT fk_budget_period_events_budget;
    END IF;
END $$;

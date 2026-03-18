-- Migration: Add trialing and paused subscription status values
-- Issues #747 #748: Paddle webhook handlers need these statuses
--
-- Extends the valid_subscription_status constraint to include:
-- - trialing: Paid subscription in trial period (Paddle status)
-- - paused: Subscription temporarily paused by user

-- Drop and re-add constraint with extended values (idempotent via DROP IF EXISTS)
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial', 'trialing', 'paused'));

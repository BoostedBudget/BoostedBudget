-- First-party checkout coordination only; these records never authorize access.
CREATE TABLE IF NOT EXISTS revenuecat_checkout_intents (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    closed_at timestamptz
);
CREATE INDEX IF NOT EXISTS revenuecat_checkout_intents_user_idx ON revenuecat_checkout_intents(user_id);
CREATE INDEX IF NOT EXISTS revenuecat_checkout_intents_open_idx ON revenuecat_checkout_intents(user_id,expires_at) WHERE closed_at IS NULL;

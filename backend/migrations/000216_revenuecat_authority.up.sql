-- Confirmed access and supplemental billing are separate; desired operations never grant access.
CREATE TABLE IF NOT EXISTS revenuecat_customers (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    grants jsonb NOT NULL DEFAULT '[]',
    access_updated_at timestamptz,
    billing text NOT NULL DEFAULT '', -- FieldEncryptor ciphertext; never query identifiers in SQL.
    billing_updated_at timestamptz,
    access_error text NOT NULL DEFAULT '',
    billing_error text NOT NULL DEFAULT '',
    next_refresh_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS revenuecat_customers_refresh_idx ON revenuecat_customers(next_refresh_at);

-- Durable desired state and audit: keep completed and superseded records until GDPR erasure.
CREATE TABLE IF NOT EXISTS revenuecat_operations (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    class text NOT NULL CHECK (class IN ('trial','comp')),
    entitlement_key text NOT NULL,
    expires_at timestamptz,
    revoke boolean NOT NULL DEFAULT false,
    version bigint NOT NULL,
    actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
    reason text NOT NULL,
    stage text NOT NULL DEFAULT 'pending' CHECK (stage IN ('pending','applied','superseded')),
    attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(user_id,class,version)
);
CREATE INDEX IF NOT EXISTS revenuecat_operations_pending_idx ON revenuecat_operations(attempts,created_at) WHERE stage='pending';
CREATE INDEX IF NOT EXISTS revenuecat_operations_user_idx ON revenuecat_operations(user_id);
CREATE INDEX IF NOT EXISTS revenuecat_operations_actor_idx ON revenuecat_operations(actor_id) WHERE actor_id IS NOT NULL;

-- No event body, email, Clerk identifier, payment details or authorization is persisted.
CREATE TABLE IF NOT EXISTS revenuecat_webhooks (
    event_id text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS revenuecat_webhooks_user_idx ON revenuecat_webhooks(user_id);
CREATE INDEX IF NOT EXISTS revenuecat_webhooks_pending_idx ON revenuecat_webhooks(received_at) WHERE processed_at IS NULL;

ALTER TABLE revenuecat_webhooks ADD COLUMN IF NOT EXISTS deliveries integer NOT NULL DEFAULT 1;
ALTER TABLE revenuecat_webhooks ADD COLUMN IF NOT EXISTS last_received_at timestamptz NOT NULL DEFAULT now();

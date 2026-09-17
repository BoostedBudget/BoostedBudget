-- Durable intent survives provider failures and cancelled GDPR requests. The
-- existing user FK erases these personal fields only with the final cascade.
ALTER TABLE revenuecat_customers ADD COLUMN IF NOT EXISTS erasure_started_at timestamptz;
ALTER TABLE revenuecat_customers ADD COLUMN IF NOT EXISTS erasure_confirmed_at timestamptz;

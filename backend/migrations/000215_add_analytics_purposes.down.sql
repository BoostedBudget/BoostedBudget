-- Reverses 000215_add_analytics_purposes.up.sql.
-- DROP COLUMN IF EXISTS is idempotent; the down migration is excluded from Squawk's
-- ban-drop-column rule via .squawk.toml's `*.down.sql` excluded_paths entry.

ALTER TABLE privacy_settings DROP COLUMN IF EXISTS gdpr_interaction_capture_consent;
ALTER TABLE privacy_settings DROP COLUMN IF EXISTS identified_diagnostics_enabled;

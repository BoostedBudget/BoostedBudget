-- #1683: add the two consent/purpose columns backing the consent-gated PostHog surfaces.
--
-- identified_diagnostics_enabled: opt-OUT (default TRUE), mirroring include_in_aggregate_stats
-- (000091). It gates attaching the user's NAME (never email) to their PostHog person profile.
-- There is deliberately NO automatic downgrade of any other column in this migration: the
-- analytics -> interaction-capture cascade lives in domain.UpdatePrivacySettingsRequest.ApplyTo,
-- where it is delta-triggered by the request shape and validated against the merged state.
--
-- gdpr_interaction_capture_consent: opt-IN (default FALSE). It gates session replay and can only
-- be true while gdpr_analytics_consent is true (enforced in ApplyTo + Validate, not in DDL).
--
-- Idempotent: each column is added inside its own DO $$ block that checks
-- information_schema.columns first, mirroring
-- 000091_add_include_in_aggregate_stats_to_privacy_settings.up.sql.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'privacy_settings'
          AND column_name = 'identified_diagnostics_enabled'
    ) THEN
        ALTER TABLE privacy_settings
            ADD COLUMN identified_diagnostics_enabled BOOLEAN NOT NULL DEFAULT TRUE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'privacy_settings'
          AND column_name = 'gdpr_interaction_capture_consent'
    ) THEN
        ALTER TABLE privacy_settings
            ADD COLUMN gdpr_interaction_capture_consent BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

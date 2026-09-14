-- #1650: prevent a surviving Clerk session from recreating Support data after local erasure.
ALTER TABLE posthog_erasure_outbox
    ADD COLUMN IF NOT EXISTS clerk_user_id_encrypted TEXT,
    ADD COLUMN IF NOT EXISTS clerk_user_id_blind_idx TEXT,
    ADD COLUMN IF NOT EXISTS clerk_deleted_at TIMESTAMPTZ;

DO $$ BEGIN
    ALTER TABLE posthog_erasure_outbox
        ADD CONSTRAINT ck_posthog_erasure_outbox_clerk_ciphertext
        CHECK (clerk_user_id_encrypted IS NULL OR clerk_user_id_encrypted LIKE 'ENC$%');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DROP INDEX IF EXISTS uq_posthog_erasure_outbox_clerk_fence;
CREATE UNIQUE INDEX uq_posthog_erasure_outbox_clerk_fence
    ON posthog_erasure_outbox (clerk_user_id_blind_idx)
    WHERE clerk_user_id_blind_idx IS NOT NULL;

ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS ck_posthog_erasure_outbox_phase;
ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT ck_posthog_erasure_outbox_phase
    CHECK (phase IN ('clerk_delete', 'person_resolve', 'person_baseline', 'person_delete',
                     'person_poll', 'support_manual', 'complete'));

ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS ck_posthog_erasure_outbox_completion;
ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT ck_posthog_erasure_outbox_completion CHECK (
    (state <> 'completed' AND completed_at IS NULL AND identities_encrypted IS NOT NULL)
    OR
    (state = 'completed' AND phase = 'complete' AND completed_at IS NOT NULL
     AND clerk_deleted_at IS NOT NULL AND clerk_user_id_encrypted IS NULL
     AND identities_encrypted IS NULL AND provider_person_uuids_encrypted IS NULL
     AND provider_deletion_baseline_encrypted IS NULL
     AND person_verified_at IS NOT NULL AND events_verified_at IS NOT NULL
     AND recordings_verified_at IS NOT NULL AND surveys_verified_at IS NOT NULL
     AND support_verified_at IS NOT NULL)
);

COMMENT ON COLUMN posthog_erasure_outbox.clerk_user_id_blind_idx IS
    'Keyed, non-recoverable fence retained after erasure to prevent re-registration of the same Clerk subject.';

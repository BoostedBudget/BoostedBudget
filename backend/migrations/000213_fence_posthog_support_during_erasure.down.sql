DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM posthog_erasure_outbox) THEN
        RAISE EXCEPTION 'REFUSING TO ROLL BACK 000213: durable erasure obligations or completed Clerk fences exist';
    END IF;
END $$;

ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS ck_posthog_erasure_outbox_completion;
ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS ck_posthog_erasure_outbox_phase;
DROP INDEX IF EXISTS uq_posthog_erasure_outbox_clerk_fence;
ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS ck_posthog_erasure_outbox_clerk_ciphertext;
ALTER TABLE posthog_erasure_outbox
    DROP COLUMN IF EXISTS clerk_deleted_at,
    DROP COLUMN IF EXISTS clerk_user_id_blind_idx,
    DROP COLUMN IF EXISTS clerk_user_id_encrypted;

ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT ck_posthog_erasure_outbox_phase
    CHECK (phase IN ('person_resolve', 'person_baseline', 'person_delete', 'person_poll',
                     'support_manual', 'complete'));
ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT ck_posthog_erasure_outbox_completion CHECK (
    (state <> 'completed' AND completed_at IS NULL AND identities_encrypted IS NOT NULL)
    OR
    (state = 'completed' AND phase = 'complete' AND completed_at IS NOT NULL
     AND identities_encrypted IS NULL AND provider_person_uuids_encrypted IS NULL
     AND provider_deletion_baseline_encrypted IS NULL
     AND person_verified_at IS NOT NULL AND events_verified_at IS NOT NULL
     AND recordings_verified_at IS NOT NULL AND surveys_verified_at IS NOT NULL
     AND support_verified_at IS NOT NULL)
);

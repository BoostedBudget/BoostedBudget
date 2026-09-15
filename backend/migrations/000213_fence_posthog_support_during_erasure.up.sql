-- #1650: prevent a surviving Clerk session from recreating Support data after local erasure.
ALTER TABLE posthog_erasure_outbox
    ADD COLUMN IF NOT EXISTS clerk_user_id_encrypted TEXT,
    ADD COLUMN IF NOT EXISTS clerk_user_id_blind_idx TEXT,
    ADD COLUMN IF NOT EXISTS clerk_deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS clerk_fence_required BOOLEAN;

-- Migration 211 could create obligations before Support credential fencing existed. Those rows
-- cannot be backfilled after the users cascade destroyed their Clerk subject, and no credential
-- issued by this not-yet-deployed feature exists to fence. Grandfather only those pre-213 rows;
-- every row inserted after this migration receives TRUE from the default.
UPDATE posthog_erasure_outbox SET clerk_fence_required = FALSE
WHERE clerk_fence_required IS NULL;
ALTER TABLE posthog_erasure_outbox
    ALTER COLUMN clerk_fence_required SET DEFAULT TRUE,
    -- The NOT NULL validation scan is bounded: this outbox holds at most one row per in-flight
    -- erasure obligation, the UPDATE above has filled every existing row, and the new DEFAULT
    -- covers concurrent inserts, so the scan cannot block reads for a meaningful window.
    --
    -- Placement is load-bearing: squawk binds this directive to the ALTER action it precedes,
    -- so above the ALTER TABLE (the convention elsewhere in this directory, e.g. 000172) it
    -- does not suppress this rule.
    -- squawk-ignore adding-not-nullable-field
    ALTER COLUMN clerk_fence_required SET NOT NULL;

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
     AND (NOT clerk_fence_required OR clerk_deleted_at IS NOT NULL)
     AND clerk_user_id_encrypted IS NULL AND clerk_user_id_blind_idx IS NULL
     AND identities_encrypted IS NULL AND provider_person_uuids_encrypted IS NULL
     AND provider_deletion_baseline_encrypted IS NULL
     AND person_verified_at IS NOT NULL AND events_verified_at IS NOT NULL
     AND recordings_verified_at IS NOT NULL AND surveys_verified_at IS NOT NULL
     AND support_verified_at IS NOT NULL)
);

COMMENT ON COLUMN posthog_erasure_outbox.clerk_user_id_blind_idx IS
    'Temporary keyed fence retained only until durable Clerk deletion; cleared by the Clerk checkpoint.';
COMMENT ON COLUMN posthog_erasure_outbox.clerk_fence_required IS
    'False only for obligations created before migration 213, when Support identity issuance was not deployed.';

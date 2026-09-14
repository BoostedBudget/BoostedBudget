-- #1650: durable controller-side record for PostHog right-to-erasure work.
-- Deliberately has no FK to users: the row must survive the local GDPR cascade.
CREATE TABLE IF NOT EXISTS posthog_erasure_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL,
    identities_encrypted TEXT,
    provider_person_uuids_encrypted TEXT,
    provider_deletion_baseline_encrypted TEXT,
    project_id TEXT NOT NULL,
    region TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    phase TEXT NOT NULL DEFAULT 'person_resolve',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error_class TEXT,
    provider_request_id TEXT,
    provider_status TEXT,
    provider_request_started_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    requires_manual_event_verification BOOLEAN NOT NULL DEFAULT FALSE,
    person_verified_at TIMESTAMPTZ,
    events_verified_at TIMESTAMPTZ,
    events_first_verified_at TIMESTAMPTZ,
    events_evidence TEXT,
    recordings_first_verified_at TIMESTAMPTZ,
    recordings_verified_at TIMESTAMPTZ,
    recordings_evidence TEXT,
    surveys_first_verified_at TIMESTAMPTZ,
    surveys_verified_at TIMESTAMPTZ,
    surveys_evidence TEXT,
    support_first_verified_at TIMESTAMPTZ,
    support_verified_at TIMESTAMPTZ,
    support_evidence TEXT,
    completed_at TIMESTAMPTZ,
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_posthog_erasure_outbox_idempotency UNIQUE (idempotency_key),
    CONSTRAINT ck_posthog_erasure_outbox_ciphertext CHECK (identities_encrypted IS NULL OR identities_encrypted LIKE 'ENC$%'),
    CONSTRAINT ck_posthog_erasure_outbox_person_ciphertext CHECK (provider_person_uuids_encrypted IS NULL OR provider_person_uuids_encrypted LIKE 'ENC$%'),
    CONSTRAINT ck_posthog_erasure_outbox_baseline_ciphertext CHECK (provider_deletion_baseline_encrypted IS NULL OR provider_deletion_baseline_encrypted LIKE 'ENC$%'),
    CONSTRAINT ck_posthog_erasure_outbox_state CHECK (state IN ('pending', 'retrying', 'manual_pending', 'completed', 'dead_letter')),
    CONSTRAINT ck_posthog_erasure_outbox_phase CHECK (phase IN ('person_resolve', 'person_delete', 'person_poll', 'support_manual', 'complete')),
    CONSTRAINT ck_posthog_erasure_outbox_attempts CHECK (attempt_count >= 0),
    CONSTRAINT ck_posthog_erasure_outbox_evidence CHECK (recordings_evidence IS NULL OR recordings_evidence IN ('provider_zero', 'provider_case_deleted')),
    CONSTRAINT ck_posthog_erasure_outbox_events_evidence CHECK (events_evidence IS NULL OR events_evidence IN ('provider_zero', 'provider_case_deleted')),
    CONSTRAINT ck_posthog_erasure_outbox_surveys_evidence CHECK (surveys_evidence IS NULL OR surveys_evidence IN ('provider_zero', 'provider_case_deleted')),
    CONSTRAINT ck_posthog_erasure_outbox_support_evidence CHECK (support_evidence IS NULL OR support_evidence IN ('provider_zero', 'provider_case_deleted')),
    CONSTRAINT ck_posthog_erasure_outbox_completion CHECK (
        (state <> 'completed' AND completed_at IS NULL AND identities_encrypted IS NOT NULL)
        OR
        (state = 'completed' AND phase = 'complete' AND completed_at IS NOT NULL
         AND identities_encrypted IS NULL AND provider_person_uuids_encrypted IS NULL
         AND provider_deletion_baseline_encrypted IS NULL
         AND person_verified_at IS NOT NULL AND events_verified_at IS NOT NULL
         AND recordings_verified_at IS NOT NULL AND surveys_verified_at IS NOT NULL
         AND support_verified_at IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS posthog_erasure_operator_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    erasure_id UUID NOT NULL REFERENCES posthog_erasure_outbox(id) ON DELETE RESTRICT,
    operator_hash TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('retarget', 'replay', 'surface_evidence', 'identity_reveal')),
    surface TEXT CHECK (surface IS NULL OR surface IN ('events', 'recordings', 'surveys', 'support')),
    evidence TEXT CHECK (evidence IS NULL OR evidence IN ('provider_zero', 'provider_case_deleted')),
    from_project_id TEXT,
    from_region TEXT,
    to_project_id TEXT,
    to_region TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_posthog_erasure_outbox_due ON posthog_erasure_outbox (next_attempt_at, requested_at, id) WHERE state IN ('pending', 'retrying');
CREATE INDEX IF NOT EXISTS idx_posthog_erasure_outbox_manual ON posthog_erasure_outbox (requested_at, id) WHERE state = 'manual_pending';
CREATE INDEX IF NOT EXISTS idx_posthog_erasure_operator_audit_erasure ON posthog_erasure_operator_audit (erasure_id, created_at, id);
COMMENT ON TABLE posthog_erasure_outbox IS 'Encrypted, durable PostHog GDPR erasure obligations. No users FK by design; survives local erasure.';

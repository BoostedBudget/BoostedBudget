-- #1650: durable controller-side record for PostHog right-to-erasure work.
-- Deliberately has no FK to users: the row must survive the local GDPR cascade.
CREATE TABLE IF NOT EXISTS posthog_erasure_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL,
    identities_encrypted TEXT NOT NULL,
    project_id TEXT NOT NULL,
    region TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    phase TEXT NOT NULL DEFAULT 'person_delete',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error_class TEXT,
    provider_request_id TEXT,
    provider_status TEXT,
    accepted_at TIMESTAMPTZ,
    first_zero_verified_at TIMESTAMPTZ,
    person_verified_at TIMESTAMPTZ,
    events_verified_at TIMESTAMPTZ,
    recordings_verified_at TIMESTAMPTZ,
    surveys_verified_at TIMESTAMPTZ,
    support_verified_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_posthog_erasure_outbox_idempotency UNIQUE (idempotency_key),
    CONSTRAINT ck_posthog_erasure_outbox_ciphertext CHECK (identities_encrypted LIKE 'ENC$%'),
    CONSTRAINT ck_posthog_erasure_outbox_state CHECK (state IN ('pending', 'retrying', 'manual_pending', 'completed', 'dead_letter')),
    CONSTRAINT ck_posthog_erasure_outbox_phase CHECK (phase IN ('person_delete', 'person_poll', 'support_manual', 'complete')),
    CONSTRAINT ck_posthog_erasure_outbox_attempts CHECK (attempt_count >= 0),
    CONSTRAINT ck_posthog_erasure_outbox_completion CHECK (
        (state <> 'completed' AND completed_at IS NULL)
        OR
        (state = 'completed' AND phase = 'complete' AND completed_at IS NOT NULL
         AND person_verified_at IS NOT NULL AND events_verified_at IS NOT NULL
         AND recordings_verified_at IS NOT NULL AND surveys_verified_at IS NOT NULL
         AND support_verified_at IS NOT NULL AND first_zero_verified_at IS NOT NULL
         AND completed_at >= first_zero_verified_at + interval '5 minutes')
    )
);

CREATE INDEX IF NOT EXISTS idx_posthog_erasure_outbox_due
    ON posthog_erasure_outbox (next_attempt_at, requested_at, id)
    WHERE state IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS idx_posthog_erasure_outbox_manual
    ON posthog_erasure_outbox (requested_at, id)
    WHERE state = 'manual_pending';

COMMENT ON TABLE posthog_erasure_outbox IS
    'Encrypted, durable PostHog GDPR erasure obligations. No users FK by design; survives local erasure.';

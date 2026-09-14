package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

type postHogErasureEnvelope struct {
	Ciphertext     string
	IdempotencyKey string
}

func buildPostHogErasureEnvelope(enc *appcrypto.FieldEncryptor, internalUserID string, encryptedClerkIDs []string) (postHogErasureEnvelope, error) {
	if enc == nil {
		return postHogErasureEnvelope{}, fmt.Errorf("PostHog erasure envelope requires field encryption")
	}
	seen := map[string]bool{}
	identities := make([]string, 0, len(encryptedClerkIDs)+1)
	appendUnique := func(value string) {
		if value != "" && !seen[value] {
			seen[value] = true
			identities = append(identities, value)
		}
	}
	appendUnique(internalUserID) // legacy backend distinct ID used before #1650
	for _, encrypted := range encryptedClerkIDs {
		plain, err := enc.DecryptString(encrypted)
		if err != nil {
			return postHogErasureEnvelope{}, fmt.Errorf("decrypt historical PostHog identity: %w", err)
		}
		appendUnique(plain)
	}
	raw, err := json.Marshal(identities)
	if err != nil {
		return postHogErasureEnvelope{}, fmt.Errorf("encode PostHog erasure identities: %w", err)
	}
	ciphertext, err := enc.EncryptJSON(raw)
	if err != nil {
		return postHogErasureEnvelope{}, fmt.Errorf("encrypt PostHog erasure identities: %w", err)
	}
	return postHogErasureEnvelope{
		Ciphertext:     ciphertext,
		IdempotencyKey: enc.BlindIndexWithLabel("posthog_erasure_v1", internalUserID),
	}, nil
}

type PostHogErasureRecord struct {
	ID                   string
	Identities           []string
	ProjectID            string
	Region               string
	State                string
	Phase                string
	AttemptCount         int
	LeaseToken           string
	FirstZeroVerifiedAt  *time.Time
	PersonVerifiedAt     *time.Time
	EventsVerifiedAt     *time.Time
	RecordingsVerifiedAt *time.Time
	SurveysVerifiedAt    *time.Time
	SupportVerifiedAt    *time.Time
}

type PostHogErasureBacklog struct {
	Counts        map[string]int64
	OldestSeconds float64
}

type PostHogErasureOutboxRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

func NewPostHogErasureOutboxRepository(db DB, enc *appcrypto.FieldEncryptor) *PostHogErasureOutboxRepository {
	return &PostHogErasureOutboxRepository{db: db, enc: enc}
}

func (r *PostHogErasureOutboxRepository) Backlog(ctx context.Context, now time.Time) (PostHogErasureBacklog, error) {
	var pending, retrying, manual, dead, completed int64
	var oldest float64
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE state='pending'),
		       COUNT(*) FILTER (WHERE state='retrying'),
		       COUNT(*) FILTER (WHERE state='manual_pending'),
		       COUNT(*) FILTER (WHERE state='dead_letter'),
		       COUNT(*) FILTER (WHERE state='completed'),
		       COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(requested_at) FILTER (WHERE state <> 'completed'))), 0)
		FROM posthog_erasure_outbox`, now).Scan(&pending, &retrying, &manual, &dead, &completed, &oldest)
	if err != nil {
		return PostHogErasureBacklog{}, fmt.Errorf("read PostHog erasure backlog: %w", err)
	}
	return PostHogErasureBacklog{Counts: map[string]int64{
		"pending": pending, "retrying": retrying, "manual_pending": manual,
		"dead_letter": dead, "completed": completed,
	}, OldestSeconds: max(oldest, 0)}, nil
}

func (r *PostHogErasureOutboxRepository) ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*PostHogErasureRecord, error) {
	leaseToken := uuid.NewString()
	var record PostHogErasureRecord
	var encrypted string
	err := r.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM posthog_erasure_outbox
			WHERE state IN ('pending', 'retrying')
			  AND next_attempt_at <= $1
			  AND (lease_until IS NULL OR lease_until <= $1)
			ORDER BY next_attempt_at, requested_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE posthog_erasure_outbox o
		SET lease_token = $2, lease_until = $1 + $3::interval,
		    attempt_count = attempt_count + 1, updated_at = clock_timestamp(), version = version + 1
		FROM candidate WHERE o.id = candidate.id
		RETURNING o.id, o.identities_encrypted, o.project_id, o.region, o.state, o.phase,
		          o.attempt_count, o.first_zero_verified_at, o.person_verified_at,
		          o.events_verified_at, o.recordings_verified_at, o.surveys_verified_at,
		          o.support_verified_at`, now, leaseToken, lease.String()).Scan(
		&record.ID, &encrypted, &record.ProjectID, &record.Region, &record.State, &record.Phase,
		&record.AttemptCount, &record.FirstZeroVerifiedAt, &record.PersonVerifiedAt,
		&record.EventsVerifiedAt, &record.RecordingsVerifiedAt, &record.SurveysVerifiedAt,
		&record.SupportVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim PostHog erasure outbox: %w", err)
	}
	record.LeaseToken = leaseToken
	raw, err := r.enc.DecryptJSON(encrypted)
	if err != nil || json.Unmarshal(raw, &record.Identities) != nil || len(record.Identities) == 0 {
		if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "identity_decryption"); deadLetterErr != nil {
			return nil, deadLetterErr
		}
		return nil, fmt.Errorf("claim PostHog erasure failed: identity_decryption")
	}
	return &record, nil
}

func (r *PostHogErasureOutboxRepository) deadLetterClaim(ctx context.Context, id, leaseToken, errorClass string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET state='dead_letter', last_error_class=$3, lease_token=NULL, lease_until=NULL,
		    updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying')`, id, leaseToken, errorClass)
	if err != nil {
		return fmt.Errorf("dead-letter unreadable PostHog erasure claim: %w", err)
	}
	return nil
}

// MarkAccepted records only that PostHog accepted the documented asynchronous person/event/
// recording deletion request. Support has no documented server deletion endpoint, so acceptance
// deliberately transitions to manual_pending and can never be mistaken for completion.
func (r *PostHogErasureOutboxRepository) MarkAccepted(ctx context.Context, id, leaseToken string, now time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET state='manual_pending', phase='support_manual', provider_status='accepted', accepted_at=$3,
		    lease_token=NULL, lease_until=NULL, last_error_class=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying')`, id, leaseToken, now)
	if err != nil {
		return false, fmt.Errorf("record PostHog erasure acceptance: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostHogErasureOutboxRepository) RecordRetry(ctx context.Context, id, leaseToken, errorClass string, next time.Time, maxAttempts int) error {
	if maxAttempts < 1 {
		return fmt.Errorf("max attempts must be positive")
	}
	_, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET state=CASE WHEN attempt_count >= $5 THEN 'dead_letter' ELSE 'retrying' END,
		    next_attempt_at=$4, last_error_class=$3, lease_token=NULL, lease_until=NULL,
		    updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying')`, id, leaseToken, errorClass, next, maxAttempts)
	if err != nil {
		return fmt.Errorf("record PostHog erasure retry: %w", err)
	}
	return nil
}

// RecordFirstZeroVerification persists the first complete, cross-surface zero result. A second
// complete verification at least five minutes later is required before completion.
func (r *PostHogErasureOutboxRepository) RecordFirstZeroVerification(ctx context.Context, id string, verifiedAt time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET first_zero_verified_at=$2, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND state='manual_pending' AND accepted_at IS NOT NULL
		  AND first_zero_verified_at IS NULL`, id, verifiedAt)
	if err != nil {
		return false, fmt.Errorf("record first PostHog erasure verification: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// CompleteWithOperatorEvidence is intentionally explicit: until an operator verifies the async
// person/event/recording job, survey-response events, and Support tickets/messages, no state-machine
// path can set completed. The evidence timestamps are durable but contain no identity or free text.
func (r *PostHogErasureOutboxRepository) CompleteWithOperatorEvidence(ctx context.Context, id string, verifiedAt time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET state='completed', phase='complete', completed_at=$2,
		    person_verified_at=$2, events_verified_at=$2, recordings_verified_at=$2,
		    surveys_verified_at=$2, support_verified_at=$2,
		    last_error_class=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND state='manual_pending' AND accepted_at IS NOT NULL
		  AND first_zero_verified_at <= $2 - interval '5 minutes'`, id, verifiedAt)
	if err != nil {
		return false, fmt.Errorf("complete PostHog erasure with operator evidence: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

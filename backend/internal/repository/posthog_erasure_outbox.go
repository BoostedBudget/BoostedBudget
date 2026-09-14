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
	ID                              string
	Identities                      []string
	PersonUUIDs                     []string
	ProjectID                       string
	Region                          string
	Phase                           string
	AttemptCount                    int
	ResolutionCursor                int
	BaselineCursor                  int
	PollCursor                      int
	LeaseToken                      string
	RequiresManualEventVerification bool
	ProviderRequestStartedAt        *time.Time
	AcceptedAt                      *time.Time
	DeletionBaseline                map[string]*time.Time
}

type PostHogErasureBacklog struct {
	Counts        map[string]int64
	OldestSeconds float64
}

type PostHogErasureOperatorRow struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	Region                 string    `json:"region"`
	State                  string    `json:"state"`
	Phase                  string    `json:"phase"`
	RequestedAt            time.Time `json:"requested_at"`
	RequiredManualSurfaces []string  `json:"required_manual_surfaces" doc:"Provider surfaces requiring twice-separated operator evidence before completion"`
}

type PostHogErasureOperatorIdentity struct {
	DistinctIDs []string `json:"distinct_ids"`
	PersonUUIDs []string `json:"person_uuids"`
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
	var encryptedPersonUUIDs *string
	var encryptedBaseline *string
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
		RETURNING o.id, o.identities_encrypted, o.provider_person_uuids_encrypted,
		          o.provider_deletion_baseline_encrypted,
		          o.project_id, o.region, o.phase, o.attempt_count, o.resolution_cursor,
		          o.baseline_cursor, o.poll_cursor,
		          o.requires_manual_event_verification, o.provider_request_started_at, o.accepted_at`, now, leaseToken, lease.String()).Scan(
		&record.ID, &encrypted, &encryptedPersonUUIDs, &encryptedBaseline, &record.ProjectID, &record.Region,
		&record.Phase, &record.AttemptCount, &record.ResolutionCursor, &record.BaselineCursor, &record.PollCursor,
		&record.RequiresManualEventVerification,
		&record.ProviderRequestStartedAt, &record.AcceptedAt)
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
	if encryptedPersonUUIDs != nil {
		raw, err = r.enc.DecryptJSON(*encryptedPersonUUIDs)
		if err != nil || json.Unmarshal(raw, &record.PersonUUIDs) != nil || (record.Phase == "person_poll" && len(record.PersonUUIDs) == 0) {
			if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "correlation_decryption"); deadLetterErr != nil {
				return nil, deadLetterErr
			}
			return nil, fmt.Errorf("claim PostHog erasure failed: correlation_decryption")
		}
	} else if record.Phase != "person_resolve" {
		if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "correlation_decryption"); deadLetterErr != nil {
			return nil, deadLetterErr
		}
		return nil, fmt.Errorf("claim PostHog erasure failed: correlation_decryption")
	}
	if record.ResolutionCursor < 0 || record.ResolutionCursor > len(record.Identities) {
		if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "invalid_correlation"); deadLetterErr != nil {
			return nil, deadLetterErr
		}
		return nil, fmt.Errorf("claim PostHog erasure failed: invalid_correlation")
	}
	if record.BaselineCursor < 0 || record.BaselineCursor > len(record.PersonUUIDs) ||
		record.PollCursor < 0 || record.PollCursor > len(record.PersonUUIDs) {
		if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "invalid_correlation"); deadLetterErr != nil {
			return nil, deadLetterErr
		}
		return nil, fmt.Errorf("claim PostHog erasure failed: invalid_correlation")
	}
	if encryptedBaseline != nil {
		raw, err = r.enc.DecryptJSON(*encryptedBaseline)
		if err != nil || json.Unmarshal(raw, &record.DeletionBaseline) != nil {
			if deadLetterErr := r.deadLetterClaim(ctx, record.ID, leaseToken, "correlation_decryption"); deadLetterErr != nil {
				return nil, deadLetterErr
			}
			return nil, fmt.Errorf("claim PostHog erasure failed: correlation_decryption")
		}
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
func (r *PostHogErasureOutboxRepository) MarkPersonUUIDsResolved(ctx context.Context, id, leaseToken string, personUUIDs []string, hasUnresolved bool, nextCursor, identityCount int) (bool, error) {
	if nextCursor < 1 || nextCursor > identityCount {
		return false, fmt.Errorf("invalid PostHog resolution cursor")
	}
	raw, err := json.Marshal(personUUIDs)
	if err != nil {
		return false, fmt.Errorf("encode PostHog person UUIDs: %w", err)
	}
	ciphertext, err := r.enc.EncryptJSON(raw)
	if err != nil {
		return false, fmt.Errorf("encrypt PostHog person UUIDs: %w", err)
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox SET provider_person_uuids_encrypted=$3,
		    requires_manual_event_verification=requires_manual_event_verification OR $4,
		    resolution_cursor=$5,
		    phase=CASE WHEN $5::integer<$6::integer THEN 'person_resolve'
		               WHEN $7::boolean THEN 'person_baseline'
		               ELSE 'person_delete' END,
		    state='retrying', next_attempt_at=clock_timestamp(), lease_token=NULL, lease_until=NULL,
		    attempt_count=0, last_error_class=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying')`, id, leaseToken, ciphertext, hasUnresolved, nextCursor, identityCount, len(personUUIDs) > 0)
	if err != nil {
		return false, fmt.Errorf("record PostHog person UUID resolution: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostHogErasureOutboxRepository) MarkDeletionBaselineCheckpoint(ctx context.Context, id, leaseToken string, baseline map[string]*time.Time, nextCursor, personCount int) (bool, error) {
	if nextCursor < 1 || nextCursor > personCount {
		return false, fmt.Errorf("invalid PostHog baseline cursor")
	}
	raw, err := json.Marshal(baseline)
	if err != nil {
		return false, fmt.Errorf("encode PostHog deletion baseline: %w", err)
	}
	ciphertext, err := r.enc.EncryptJSON(raw)
	if err != nil {
		return false, fmt.Errorf("encrypt PostHog deletion baseline: %w", err)
	}
	hasExisting := false
	for _, createdAt := range baseline {
		if createdAt != nil {
			hasExisting = true
			break
		}
	}
	tag, err := r.db.Exec(ctx, `UPDATE posthog_erasure_outbox SET
		provider_deletion_baseline_encrypted=$3, baseline_cursor=$4,
		requires_manual_event_verification=requires_manual_event_verification OR $6,
		phase=CASE WHEN $4::integer=$5::integer THEN 'person_delete' ELSE 'person_baseline' END,
		state='retrying',next_attempt_at=clock_timestamp(),attempt_count=0,last_error_class=NULL,
		lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp(),version=version+1
		WHERE id=$1 AND lease_token=$2 AND phase='person_baseline'`, id, leaseToken, ciphertext, nextCursor, personCount, hasExisting)
	if err != nil {
		return false, fmt.Errorf("checkpoint PostHog deletion baseline: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostHogErasureOutboxRepository) MarkDeletionRequestStarted(ctx context.Context, id, leaseToken string, baseline map[string]*time.Time) (time.Time, bool, error) {
	raw, err := json.Marshal(baseline)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("encode PostHog deletion baseline: %w", err)
	}
	ciphertext, err := r.enc.EncryptJSON(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("encrypt PostHog deletion baseline: %w", err)
	}
	hasExisting := false
	for _, createdAt := range baseline {
		if createdAt != nil {
			hasExisting = true
			break
		}
	}
	var started time.Time
	err = r.db.QueryRow(ctx, `UPDATE posthog_erasure_outbox
		SET provider_request_started_at=COALESCE(provider_request_started_at,clock_timestamp()),
		    provider_deletion_baseline_encrypted=COALESCE(provider_deletion_baseline_encrypted,$3),
		    requires_manual_event_verification=requires_manual_event_verification OR $4,
		    updated_at=clock_timestamp(),version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying') AND phase='person_delete'
		RETURNING provider_request_started_at`, id, leaseToken, ciphertext, hasExisting).Scan(&started)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("record PostHog deletion request start: %w", err)
	}
	return started, true, nil
}

func (r *PostHogErasureOutboxRepository) MarkAccepted(ctx context.Context, id, leaseToken string, resolvedPersons int, requiresManualEvents bool) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE posthog_erasure_outbox
		SET state=CASE WHEN $3=0 THEN 'manual_pending' ELSE 'retrying' END,
		    phase=CASE WHEN $3=0 THEN 'support_manual' ELSE 'person_poll' END,
		    provider_status='accepted', accepted_at=clock_timestamp(), person_verified_at=clock_timestamp(),
		    requires_manual_event_verification=requires_manual_event_verification OR $4,
		    poll_cursor=0,
		    next_attempt_at=clock_timestamp()+interval '5 minutes',
		    attempt_count=0, lease_token=NULL, lease_until=NULL, last_error_class=NULL,
		    updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying')`, id, leaseToken, resolvedPersons, requiresManualEvents)
	if err != nil {
		return false, fmt.Errorf("record PostHog erasure acceptance: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostHogErasureOutboxRepository) RecordRetry(ctx context.Context, id, leaseToken, errorClass string, next time.Time, maxAttempts int) (string, error) {
	if maxAttempts < 1 {
		return "", fmt.Errorf("max attempts must be positive")
	}
	var state string
	err := r.db.QueryRow(ctx, `
		UPDATE posthog_erasure_outbox
		SET state=CASE WHEN attempt_count >= $5 THEN 'dead_letter' ELSE 'retrying' END,
		    next_attempt_at=$4, last_error_class=$3, lease_token=NULL, lease_until=NULL,
		    updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND state IN ('pending','retrying') RETURNING state`, id, leaseToken, errorClass, next, maxAttempts).Scan(&state)
	if err != nil {
		return "", fmt.Errorf("record PostHog erasure retry: %w", err)
	}
	return state, nil
}

func (r *PostHogErasureOutboxRepository) RecordPollPending(ctx context.Context, id, leaseToken string) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE posthog_erasure_outbox SET state='retrying',
		next_attempt_at=clock_timestamp()+interval '6 hours', attempt_count=0, last_error_class=NULL,
		lease_token=NULL, lease_until=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND lease_token=$2 AND phase='person_poll'`, id, leaseToken)
	return tag.RowsAffected() == 1, err
}

func (r *PostHogErasureOutboxRepository) MarkPersonPollCheckpoint(ctx context.Context, id, leaseToken string, nextCursor, personCount int, requiresManualEvents bool) (bool, error) {
	if nextCursor < 1 || nextCursor > personCount {
		return false, fmt.Errorf("invalid PostHog poll cursor")
	}
	tag, err := r.db.Exec(ctx, `UPDATE posthog_erasure_outbox SET
		poll_cursor=$3,requires_manual_event_verification=requires_manual_event_verification OR $5,
		state=CASE WHEN $3::integer=$4::integer THEN 'manual_pending' ELSE 'retrying' END,
		phase=CASE WHEN $3::integer=$4::integer THEN 'support_manual' ELSE 'person_poll' END,
		events_verified_at=CASE WHEN $3::integer=$4::integer AND NOT (requires_manual_event_verification OR $5)
		                        THEN clock_timestamp() ELSE events_verified_at END,
		next_attempt_at=clock_timestamp(),attempt_count=0,last_error_class=NULL,
		lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp(),version=version+1
		WHERE id=$1 AND lease_token=$2 AND phase='person_poll'`, id, leaseToken, nextCursor, personCount, requiresManualEvents)
	if err != nil {
		return false, fmt.Errorf("checkpoint PostHog deletion poll: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RecordOperatorEvidence stores only bounded evidence, using the database clock for both the
// five-minute separation and audit time. Completion atomically destroys the last provider
// identifiers, so a completed row contains no recoverable PostHog identity.
func (r *PostHogErasureOutboxRepository) RecordOperatorEvidence(ctx context.Context, id, operatorID, surface, evidence string) (string, error) {
	if r.enc == nil {
		return "", fmt.Errorf("operator audit requires field encryption")
	}
	if evidence != "provider_zero" && evidence != "provider_case_deleted" {
		return "", fmt.Errorf("unsupported PostHog erasure evidence")
	}
	var firstColumn, verifiedColumn, evidenceColumn string
	switch surface {
	case "events":
		firstColumn, verifiedColumn, evidenceColumn = "events_first_verified_at", "events_verified_at", "events_evidence"
	case "recordings":
		firstColumn, verifiedColumn, evidenceColumn = "recordings_first_verified_at", "recordings_verified_at", "recordings_evidence"
	case "surveys":
		firstColumn, verifiedColumn, evidenceColumn = "surveys_first_verified_at", "surveys_verified_at", "surveys_evidence"
	case "support":
		firstColumn, verifiedColumn, evidenceColumn = "support_first_verified_at", "support_verified_at", "support_evidence"
	default:
		return "", fmt.Errorf("unsupported PostHog erasure surface")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin PostHog operator evidence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := fmt.Sprintf(`UPDATE posthog_erasure_outbox SET
		%s=COALESCE(%s, clock_timestamp()),
		%s=CASE WHEN %s <= clock_timestamp()-interval '5 minutes' THEN clock_timestamp() ELSE %s END,
		%s=$2, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND state='manual_pending' RETURNING state`, firstColumn, firstColumn,
		verifiedColumn, firstColumn, verifiedColumn, evidenceColumn)
	state := "manual_pending"
	if err = tx.QueryRow(ctx, query, id, evidence).Scan(&state); err != nil {
		return "", fmt.Errorf("record PostHog operator evidence: %w", err)
	}
	operatorHash := r.enc.BlindIndexWithLabel("posthog_erasure_operator_v1", operatorID)
	if _, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_operator_audit
		(erasure_id, operator_hash, action, surface, evidence)
		VALUES ($1,$2,'surface_evidence',$3,$4)`, id, operatorHash, surface, evidence); err != nil {
		return "", fmt.Errorf("audit PostHog operator evidence: %w", err)
	}
	if err = tx.QueryRow(ctx, `UPDATE posthog_erasure_outbox SET
		state='completed', phase='complete', completed_at=clock_timestamp(),
		identities_encrypted=NULL, provider_person_uuids_encrypted=NULL,
		provider_deletion_baseline_encrypted=NULL,
		last_error_class=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND state='manual_pending' AND person_verified_at IS NOT NULL
		  AND events_verified_at IS NOT NULL AND recordings_verified_at IS NOT NULL
		  AND surveys_verified_at IS NOT NULL AND support_verified_at IS NOT NULL
		RETURNING state`, id).Scan(&state); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("complete PostHog erasure: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit PostHog operator evidence: %w", err)
	}
	return state, nil
}

// RetargetUnconfigured is the only supported repair for obligations created while the provider
// target was missing. It cannot move a request already accepted by PostHog.
func (r *PostHogErasureOutboxRepository) RetargetUnconfigured(ctx context.Context, id, operatorID, projectID, region string) (bool, error) {
	if r.enc == nil || projectID == "" || (region != "us" && region != "eu") {
		return false, fmt.Errorf("invalid PostHog erasure retarget")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var priorProject, priorRegion string
	err = tx.QueryRow(ctx, `SELECT project_id,region FROM posthog_erasure_outbox
		WHERE id=$1 AND (project_id='unconfigured' OR region='unconfigured')
		  AND accepted_at IS NULL AND state IN ('pending','retrying','dead_letter') FOR UPDATE`, id).
		Scan(&priorProject, &priorRegion)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read PostHog erasure target: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE posthog_erasure_outbox SET project_id=$2, region=$3,
		state='retrying', phase='person_resolve', next_attempt_at=clock_timestamp(),
		resolution_cursor=0, baseline_cursor=0, poll_cursor=0, provider_person_uuids_encrypted=NULL,
		provider_deletion_baseline_encrypted=NULL, provider_request_started_at=NULL,
		requires_manual_event_verification=FALSE,
		last_error_class=NULL, lease_token=NULL, lease_until=NULL, updated_at=clock_timestamp(), version=version+1
		WHERE id=$1 AND accepted_at IS NULL`, id, projectID, region)
	if err != nil {
		return false, fmt.Errorf("retarget PostHog erasure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	operatorHash := r.enc.BlindIndexWithLabel("posthog_erasure_operator_v1", operatorID)
	_, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_operator_audit
		(erasure_id,operator_hash,action,from_project_id,from_region,to_project_id,to_region)
		VALUES ($1,$2,'retarget',$3,$4,$5,$6)`, id, operatorHash, priorProject, priorRegion, projectID, region)
	if err != nil {
		return false, fmt.Errorf("audit PostHog erasure retarget: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// RequeueDeadLetter preserves the attempt count, target, correlation, and encrypted identity. It
// only clears the bounded failure/lease state so an operator cannot use replay to rewrite history.
func (r *PostHogErasureOutboxRepository) RequeueDeadLetter(ctx context.Context, id, operatorID string) (bool, error) {
	if r.enc == nil {
		return false, fmt.Errorf("PostHog erasure replay requires encryption")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE posthog_erasure_outbox SET state='retrying',
		next_attempt_at=clock_timestamp(), last_error_class=NULL, lease_token=NULL, lease_until=NULL,
		updated_at=clock_timestamp(), version=version+1 WHERE id=$1 AND state='dead_letter'`, id)
	if err != nil {
		return false, fmt.Errorf("requeue PostHog erasure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	operatorHash := r.enc.BlindIndexWithLabel("posthog_erasure_operator_v1", operatorID)
	if _, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_operator_audit(erasure_id,operator_hash,action)
		VALUES($1,$2,'replay')`, id, operatorHash); err != nil {
		return false, fmt.Errorf("audit PostHog erasure replay: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PostHogErasureOutboxRepository) ListOutstanding(ctx context.Context, limit int) ([]PostHogErasureOperatorRow, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be 1..100")
	}
	rows, err := r.db.Query(ctx, `SELECT id,project_id,region,state,phase,requested_at,
		requires_manual_event_verification
		FROM posthog_erasure_outbox WHERE state<>'completed' ORDER BY requested_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list PostHog erasures: %w", err)
	}
	defer rows.Close()
	var out []PostHogErasureOperatorRow
	for rows.Next() {
		var row PostHogErasureOperatorRow
		var requiresManualEvents bool
		if err := rows.Scan(&row.ID, &row.ProjectID, &row.Region, &row.State, &row.Phase, &row.RequestedAt, &requiresManualEvents); err != nil {
			return nil, err
		}
		row.RequiredManualSurfaces = []string{"recordings", "surveys", "support"}
		if requiresManualEvents {
			row.RequiredManualSurfaces = append([]string{"events"}, row.RequiredManualSurfaces...)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// RevealOperatorIdentity is intentionally a POST: every sensitive reveal is an auditable action,
// never an incidental field on the list response.
func (r *PostHogErasureOutboxRepository) RevealOperatorIdentity(ctx context.Context, id, operatorID string) (PostHogErasureOperatorIdentity, error) {
	if r.enc == nil {
		return PostHogErasureOperatorIdentity{}, fmt.Errorf("identity reveal requires encryption")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return PostHogErasureOperatorIdentity{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var identities string
	var personUUIDs *string
	if err = tx.QueryRow(ctx, `SELECT identities_encrypted,provider_person_uuids_encrypted
		FROM posthog_erasure_outbox WHERE id=$1 AND state<>'completed' FOR UPDATE`, id).Scan(&identities, &personUUIDs); err != nil {
		return PostHogErasureOperatorIdentity{}, err
	}
	var out PostHogErasureOperatorIdentity
	raw, err := r.enc.DecryptJSON(identities)
	if err != nil || json.Unmarshal(raw, &out.DistinctIDs) != nil {
		return out, fmt.Errorf("decrypt PostHog identities")
	}
	if personUUIDs != nil {
		raw, err = r.enc.DecryptJSON(*personUUIDs)
		if err != nil || json.Unmarshal(raw, &out.PersonUUIDs) != nil {
			return out, fmt.Errorf("decrypt PostHog correlations")
		}
	}
	operatorHash := r.enc.BlindIndexWithLabel("posthog_erasure_operator_v1", operatorID)
	if _, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_operator_audit(erasure_id,operator_hash,action)
		VALUES($1,$2,'identity_reveal')`, id, operatorHash); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

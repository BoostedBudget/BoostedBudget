//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteUserCascade_Integration_PostHogOutboxSurvivesWithEncryptedHistoricalIdentities(t *testing.T) {
	pool := connectTestDB(t)
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "posthog-erasure-integration-" + suffix
	repo.SetPostHogErasureTarget(project, "us")
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	historical := "clerk_historical_" + suffix
	historicalEncrypted, err := enc.EncryptString(historical)
	require.NoError(t, err)
	providerEncrypted, err := enc.EncryptString("provider_" + suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO user_identities
		(user_id, clerk_user_id, provider, provider_user_id, is_primary, clerk_user_id_blind_idx)
		VALUES ($1, $2, 'github', $3, FALSE, $4)`,
		user.ID, historicalEncrypted, providerEncrypted, enc.BlindIndex(historical))
	require.NoError(t, err)

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)
	assertUserRowGone(t, ctx, pool, user.ID)

	var outboxID, ciphertext, clerkCiphertext string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id, identities_encrypted, clerk_user_id_encrypted
		FROM posthog_erasure_outbox WHERE project_id=$1`, project).
		Scan(&outboxID, &ciphertext, &clerkCiphertext))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", outboxID) })
	assert.NotContains(t, ciphertext, historical)
	assert.NotContains(t, ciphertext, user.ClerkUserID)
	assert.NotContains(t, clerkCiphertext, user.ClerkUserID)
	canonical, err := enc.DecryptString(clerkCiphertext)
	require.NoError(t, err)
	assert.Equal(t, user.ClerkUserID, canonical)
	fenced, err := NewPostHogErasureOutboxRepository(pool, enc).
		IsClerkIdentityErasureFenced(ctx, user.ClerkUserID)
	require.NoError(t, err)
	assert.True(t, fenced)
	raw, err := enc.DecryptJSON(ciphertext)
	require.NoError(t, err)
	var identities []string
	require.NoError(t, json.Unmarshal(raw, &identities))
	assert.ElementsMatch(t, []string{user.ID, user.ClerkUserID, historical}, identities)
}

func TestDeleteUserCascade_Integration_OutboxInsertFailureRollsBackLocalErasure(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "posthog-erasure-rollback-" + suffix
	repo.SetPostHogErasureTarget(project, "us")
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)
	constraint := "ck_posthog_erasure_rollback_" + suffix
	_, err := pool.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT %s CHECK (project_id <> '%s')", constraint, project))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, fmt.Sprintf("ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS %s", constraint))
	})

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.Error(t, err)
	assert.False(t, deleted)
	assertUserRowExists(t, ctx, pool, user.ID)
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM posthog_erasure_outbox WHERE project_id=$1", project).Scan(&count))
	assert.Zero(t, count)
}

func TestPostHogErasureOperatorCompletion_IntegrationAtomicallyDestroysIdentity(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identities, err := enc.EncryptJSON([]byte(`["clerk_secret"]`))
	require.NoError(t, err)
	personUUIDs, err := enc.EncryptJSON([]byte(`["11111111-1111-4111-8111-111111111111"]`))
	require.NoError(t, err)
	baseline, err := enc.EncryptJSON([]byte(`{"11111111-1111-4111-8111-111111111111":"2026-09-14T10:00:00Z"}`))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,clerk_user_id_encrypted,clerk_user_id_blind_idx,clerk_deleted_at,
		 provider_person_uuids_encrypted,provider_deletion_baseline_encrypted,project_id,region,state,phase,
		 accepted_at,person_verified_at,events_verified_at,recordings_first_verified_at,surveys_first_verified_at,support_first_verified_at)
		VALUES($1,$2,$3,$3,$6,clock_timestamp(),$4,$5,'42','us','manual_pending','support_manual',clock_timestamp(),clock_timestamp(),clock_timestamp(),
		 clock_timestamp()-interval '6 minutes',clock_timestamp()-interval '6 minutes',clock_timestamp()-interval '6 minutes')`,
		id, "completion:"+id, identities, personUUIDs, baseline, "fence:"+id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit WHERE erasure_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id)
	})
	for _, surface := range []string{"recordings", "surveys", "support"} {
		_, err = repo.RecordOperatorEvidence(ctx, id, uuid.NewString(), surface, "provider_zero")
		require.NoError(t, err)
	}
	var state string
	var remainingIdentities, remainingClerk, remainingUUIDs, remainingBaseline *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state,identities_encrypted,clerk_user_id_encrypted,
		provider_person_uuids_encrypted,provider_deletion_baseline_encrypted
		FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&state, &remainingIdentities, &remainingClerk, &remainingUUIDs, &remainingBaseline))
	assert.Equal(t, "completed", state)
	assert.Nil(t, remainingIdentities)
	assert.Nil(t, remainingClerk)
	assert.Nil(t, remainingUUIDs)
	assert.Nil(t, remainingBaseline)
}

func TestPostHogErasureCompletionWaitsForDurableClerkDeletion_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identity, err := enc.EncryptString("clerk_open_session")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,clerk_user_id_encrypted,clerk_user_id_blind_idx,
		 project_id,region,state,phase,person_verified_at,events_verified_at,
		 recordings_verified_at,surveys_verified_at,support_first_verified_at)
		VALUES($1,$2,$3,$3,$4,'42','us','manual_pending','support_manual',clock_timestamp(),clock_timestamp(),
		 clock_timestamp(),clock_timestamp(),clock_timestamp()-interval '6 minutes')`,
		id, "clerk-fence:"+id, identity, "fence:"+id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit WHERE erasure_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id)
	})

	state, err := repo.RecordOperatorEvidence(ctx, id, uuid.NewString(), "support", "provider_zero")
	require.NoError(t, err)
	assert.Equal(t, "manual_pending", state)

	_, err = pool.Exec(ctx, `UPDATE posthog_erasure_outbox SET clerk_deleted_at=clock_timestamp() WHERE id=$1`, id)
	require.NoError(t, err)
	state, err = repo.RecordOperatorEvidence(ctx, id, uuid.NewString(), "support", "provider_zero")
	require.NoError(t, err)
	assert.Equal(t, "completed", state)
}

func TestPostHogErasureLegacyObligationCompletesWithoutUnavailableClerkSubject_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identity, err := enc.EncryptString("legacy_identity")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,clerk_fence_required,
		 project_id,region,state,phase,person_verified_at,events_verified_at,
		 recordings_verified_at,surveys_verified_at,support_first_verified_at)
		VALUES($1,$2,$3,FALSE,'42','us','manual_pending','support_manual',clock_timestamp(),
		 clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp()-interval '6 minutes')`,
		id, "legacy-clerk-exempt:"+id, identity)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit WHERE erasure_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id)
	})

	state, err := repo.RecordOperatorEvidence(ctx, id, uuid.NewString(), "support", "provider_zero")
	require.NoError(t, err)
	assert.Equal(t, "completed", state)
}

func TestPostHogErasureClerkCheckpointSchedulesBeyondClientRevalidationWindow_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identity, err := enc.EncryptString("clerk_open_session")
	require.NoError(t, err)
	lease := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,clerk_user_id_encrypted,clerk_user_id_blind_idx,
		 project_id,region,state,phase,lease_token,lease_until)
		VALUES($1,$2,$3,$3,$4,'42','us','retrying','clerk_delete',$5,
		 clock_timestamp()+interval '1 minute')`, id, "clerk-delay:"+id, identity, "fence:"+id, lease)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id) })

	updated, err := repo.MarkClerkDeleted(ctx, id, lease)
	require.NoError(t, err)
	require.True(t, updated)
	var safelyDelayed bool
	var retainedFence *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT next_attempt_at >= clerk_deleted_at + interval '30 seconds'
		, clerk_user_id_blind_idx FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&safelyDelayed, &retainedFence))
	assert.True(t, safelyDelayed)
	assert.Nil(t, retainedFence, "the temporary keyed fence must be destroyed after live Clerk deletion")
}

func TestPostHogErasureMixedIdentity_IntegrationRequiresManualEventEvidence(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identities, err := enc.EncryptJSON([]byte(`["person","event-only"]`))
	require.NoError(t, err)
	personUUIDs, err := enc.EncryptJSON([]byte(`["11111111-1111-4111-8111-111111111111"]`))
	require.NoError(t, err)
	leaseToken := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,provider_person_uuids_encrypted,project_id,region,state,phase,
		 lease_token,lease_until,accepted_at,person_verified_at,requires_manual_event_verification)
		VALUES($1,$2,$3,$4,'42','us','retrying','person_poll',$5,clock_timestamp()+interval '1 minute',
		 clock_timestamp(),clock_timestamp(),TRUE)`, id, "mixed:"+id, identities, personUUIDs, leaseToken)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id) })

	updated, err := repo.MarkPersonPollCheckpoint(ctx, id, leaseToken, 1, 1, false)
	require.NoError(t, err)
	require.True(t, updated)
	var state, phase string
	var eventsVerifiedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT state,phase,events_verified_at FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&state, &phase, &eventsVerifiedAt))
	assert.Equal(t, "manual_pending", state)
	assert.Equal(t, "support_manual", phase)
	assert.Nil(t, eventsVerifiedAt)
}

func TestPostHogErasureRetarget_IntegrationAcceptsEitherMissingComponentAndAuditsPriorTarget(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	identities, err := enc.EncryptJSON([]byte(`["subject"]`))
	require.NoError(t, err)

	for _, tc := range []struct {
		name, priorProject, priorRegion string
	}{
		{name: "missing project", priorProject: "unconfigured", priorRegion: "us"},
		{name: "missing region", priorProject: "42", priorRegion: "unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			_, err := pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
				(id,idempotency_key,identities_encrypted,project_id,region,state,phase)
				VALUES($1,$2,$3,$4,$5,'dead_letter','person_resolve')`,
				id, "retarget:"+id, identities, tc.priorProject, tc.priorRegion)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit WHERE erasure_id=$1", id)
				_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id)
			})

			updated, err := repo.RetargetUnconfigured(ctx, id, uuid.NewString(), "84", "eu")
			require.NoError(t, err)
			require.True(t, updated)
			var project, region, state, priorProject, priorRegion string
			require.NoError(t, pool.QueryRow(ctx, `SELECT project_id,region,state FROM posthog_erasure_outbox WHERE id=$1`, id).
				Scan(&project, &region, &state))
			require.NoError(t, pool.QueryRow(ctx, `SELECT from_project_id,from_region FROM posthog_erasure_operator_audit
				WHERE erasure_id=$1 AND action='retarget'`, id).Scan(&priorProject, &priorRegion))
			assert.Equal(t, "84", project)
			assert.Equal(t, "eu", region)
			assert.Equal(t, "retrying", state)
			assert.Equal(t, tc.priorProject, priorProject)
			assert.Equal(t, tc.priorRegion, priorRegion)
		})
	}
}

func TestPostHogErasureResolutionCheckpoint_IntegrationPersistsAcrossLeases(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	leaseToken := uuid.NewString()
	identities, err := enc.EncryptJSON([]byte(`["first","second"]`))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,project_id,region,state,phase,attempt_count,lease_token,lease_until)
		VALUES($1,$2,$3,'42','us','retrying','person_resolve',7,$4,clock_timestamp()+interval '1 minute')`,
		id, "checkpoint:"+id, identities, leaseToken)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id) })

	personUUID := "11111111-1111-4111-8111-111111111111"
	updated, err := repo.MarkPersonUUIDsResolved(ctx, id, leaseToken, []string{personUUID}, true, 1, 2)
	require.NoError(t, err)
	require.True(t, updated)
	var ciphertext, phase string
	var cursor, attempts int
	var requiresManualEvents bool
	var immediatelyDue bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT provider_person_uuids_encrypted,phase,resolution_cursor,
		attempt_count,requires_manual_event_verification,next_attempt_at <= clock_timestamp()
		FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&ciphertext, &phase, &cursor, &attempts, &requiresManualEvents, &immediatelyDue))
	raw, err := enc.DecryptJSON(ciphertext)
	require.NoError(t, err)
	assert.JSONEq(t, `["`+personUUID+`"]`, string(raw))
	assert.Equal(t, "person_resolve", phase)
	assert.Equal(t, 1, cursor)
	assert.Zero(t, attempts)
	assert.True(t, requiresManualEvents)
	assert.True(t, immediatelyDue)
}

func TestPostHogErasureBaselineAndPollCheckpoints_IntegrationPersistIncrementally(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	firstUUID := "11111111-1111-4111-8111-111111111111"
	secondUUID := "22222222-2222-4222-8222-222222222222"
	identities, err := enc.EncryptJSON([]byte(`["first","second"]`))
	require.NoError(t, err)
	personUUIDs, err := enc.EncryptJSON([]byte(`["` + firstUUID + `","` + secondUUID + `"]`))
	require.NoError(t, err)
	leaseToken := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,provider_person_uuids_encrypted,project_id,region,state,phase,
		 resolution_cursor,attempt_count,lease_token,lease_until)
		VALUES($1,$2,$3,$4,'42','us','retrying','person_baseline',2,7,$5,clock_timestamp()+interval '1 minute')`,
		id, "provider-checkpoint:"+id, identities, personUUIDs, leaseToken)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id) })

	baseline := map[string]*time.Time{firstUUID: nil}
	updated, err := repo.MarkDeletionBaselineCheckpoint(ctx, id, leaseToken, baseline, 1, 2)
	require.NoError(t, err)
	require.True(t, updated)
	var baselineCiphertext, phase string
	var baselineCursor, attempts int
	var immediatelyDue bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT provider_deletion_baseline_encrypted,phase,baseline_cursor,
		attempt_count,next_attempt_at <= clock_timestamp() FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&baselineCiphertext, &phase, &baselineCursor, &attempts, &immediatelyDue))
	raw, err := enc.DecryptJSON(baselineCiphertext)
	require.NoError(t, err)
	assert.JSONEq(t, `{"`+firstUUID+`":null}`, string(raw))
	assert.Equal(t, "person_baseline", phase)
	assert.Equal(t, 1, baselineCursor)
	assert.Zero(t, attempts)
	assert.True(t, immediatelyDue)

	pollLease := uuid.NewString()
	_, err = pool.Exec(ctx, `UPDATE posthog_erasure_outbox SET phase='person_poll',state='retrying',
		accepted_at=clock_timestamp(),person_verified_at=clock_timestamp(),attempt_count=9,
		lease_token=$2,lease_until=clock_timestamp()+interval '1 minute' WHERE id=$1`, id, pollLease)
	require.NoError(t, err)
	updated, err = repo.MarkPersonPollCheckpoint(ctx, id, pollLease, 1, 2, true)
	require.NoError(t, err)
	require.True(t, updated)
	var pollCursor int
	var requiresManual bool
	var eventsVerifiedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT phase,poll_cursor,attempt_count,
		requires_manual_event_verification,events_verified_at,next_attempt_at <= clock_timestamp()
		FROM posthog_erasure_outbox WHERE id=$1`, id).
		Scan(&phase, &pollCursor, &attempts, &requiresManual, &eventsVerifiedAt, &immediatelyDue))
	assert.Equal(t, "person_poll", phase)
	assert.Equal(t, 1, pollCursor)
	assert.Zero(t, attempts)
	assert.True(t, requiresManual)
	assert.Nil(t, eventsVerifiedAt)
	assert.True(t, immediatelyDue)
}

func TestPostHogErasureSuccessfulAcceptanceAndPendingPollResetRetryBudget_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	personUUID := "33333333-3333-4333-8333-333333333333"
	identities, err := enc.EncryptJSON([]byte(`["subject"]`))
	require.NoError(t, err)
	personUUIDs, err := enc.EncryptJSON([]byte(`["` + personUUID + `"]`))
	require.NoError(t, err)
	leaseToken := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,provider_person_uuids_encrypted,project_id,region,state,phase,
		 attempt_count,last_error_class,lease_token,lease_until)
		VALUES($1,$2,$3,$4,'42','us','retrying','person_delete',11,'provider',$5,
		 clock_timestamp()+interval '1 minute')`, id, "retry-reset:"+id, identities, personUUIDs, leaseToken)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id) })

	updated, err := repo.MarkAccepted(ctx, id, leaseToken, 1, false)
	require.NoError(t, err)
	require.True(t, updated)
	assertRetryAccountingCleared(t, ctx, pool, id)
	prioritizePostHogErasureFixture(t, ctx, pool, id)

	claimed, err := repo.ClaimDue(ctx, time.Now().Add(6*time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, id, claimed.ID)
	require.Equal(t, 1, claimed.AttemptCount)

	updated, err = repo.RecordPollPending(ctx, id, claimed.LeaseToken)
	require.NoError(t, err)
	require.True(t, updated)
	assertRetryAccountingCleared(t, ctx, pool, id)
	prioritizePostHogErasureFixture(t, ctx, pool, id)

	claimed, err = repo.ClaimDue(ctx, time.Now().Add(7*time.Hour), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, id, claimed.ID)
	require.Equal(t, 1, claimed.AttemptCount)
	state, err := repo.RecordRetry(ctx, id, claimed.LeaseToken, "provider", time.Now().Add(time.Minute), 12)
	require.NoError(t, err)
	assert.Equal(t, "retrying", state)
}

func assertRetryAccountingCleared(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	var attempts int
	var lastError *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_count,last_error_class
		FROM posthog_erasure_outbox WHERE id=$1`, id).Scan(&attempts, &lastError))
	assert.Zero(t, attempts)
	assert.Nil(t, lastError)
}

// ClaimDue is intentionally global. Give this fixture the earliest possible due time so retained
// obligations left by other integration tests cannot win the claim without modifying those rows.
func prioritizePostHogErasureFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	tag, err := pool.Exec(ctx, `UPDATE posthog_erasure_outbox SET next_attempt_at='-infinity' WHERE id=$1`, id)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

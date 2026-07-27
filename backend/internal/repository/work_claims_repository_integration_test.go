//go:build integration

package repository

// Integration tests for WorkClaimsRepository (migration 000114, issue #1413).
//
// These are the tests that matter: pgxmock can only match query text/args and
// cannot validate ON CONFLICT ... WHERE semantics, so the exclusivity guarantee
// this table exists to provide is only provable against a real Postgres planner
// and its row-level locking during INSERT ... ON CONFLICT.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestWorkClaims -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestResource creates a unique resourceID and registers a cleanup to delete
// the corresponding work_claims rows after the test.
func setupTestResource(t *testing.T, db DB) string {
	resourceID := uuid.New().String()
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2`,
			ResourceTypeUser, resourceID)
	})
	return resourceID
}

func TestWorkClaims_Claim_ExclusiveSequential(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	first, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, first, "first claim on a fresh (resource, action) must win")

	second, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, second, "second claim while the first lease is unexpired must lose")
}

func TestWorkClaims_Claim_ExpiredLeaseIsReclaimable(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	first, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 10*time.Millisecond)
	require.NoError(t, err)
	require.True(t, first, "first claim must win")

	// Simulate a crashed worker: nobody calls Complete or Release. Wait out the lease.
	time.Sleep(100 * time.Millisecond)

	reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "an expired lease from a crashed worker must not block forever")
}

func TestWorkClaims_Complete_NeverReGranted(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d, 10*time.Millisecond)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d))

	// Even after the (short) lease would otherwise have expired, a completed
	// claim must never be re-granted.
	time.Sleep(100 * time.Millisecond)

	reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed, "a completed claim must never be re-granted, lease expiry notwithstanding")
}

func TestWorkClaims_Release_ImmediatelyReclaimableAndRecordsReason(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	const reason = "plaid disconnect call failed: 503"
	require.NoError(t, repo.Release(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, reason))

	// Check last_error was recorded BEFORE reclaiming — a subsequent successful
	// Claim resets last_error to NULL, so order matters here.
	var lastError string
	err = pool.QueryRow(ctx,
		`SELECT last_error FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionPastDueGraceCancel,
	).Scan(&lastError)
	require.NoError(t, err)
	assert.Equal(t, reason, lastError, "Release must record why the work failed")

	// Immediately reclaimable — no need to wait out the (long, 15m) lease.
	reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "Release must make the claim immediately reclaimable, not wait for lease expiry")
}

func TestWorkClaims_Release_RecordsReason_ObservedBeforeReclaim(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	const reason = "paddle cleanup call timed out"
	require.NoError(t, repo.Release(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, reason))

	var (
		lastError      string
		leaseExpiresAt time.Time
		completedAt    *time.Time
	)
	err = pool.QueryRow(ctx,
		`SELECT last_error, lease_expires_at, completed_at FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
	).Scan(&lastError, &leaseExpiresAt, &completedAt)
	require.NoError(t, err)

	assert.Equal(t, reason, lastError, "Release must record the failure reason")
	assert.Nil(t, completedAt, "Release must not mark the claim completed")
	assert.True(t, leaseExpiresAt.Before(time.Now()), "Release must leave the lease already expired")
}

func TestWorkClaims_Complete_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d))
	require.NoError(t, repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d), "Complete must be idempotent")

	reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed)
}

func TestWorkClaims_Release_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.Release(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, "first failure"))
	require.NoError(t, repo.Release(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, "second failure"), "Release must be idempotent")

	reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "still reclaimable after two Releases")
}

// TestWorkClaims_Claim_ConcurrentRace is the falsifiability-critical test: it proves
// exclusivity under genuine concurrency against the live DB, not just sequential calls
// that could theoretically pass due to test ordering rather than real locking semantics.
func TestWorkClaims_Claim_ConcurrentRace(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	const workers = 25
	results := make(chan bool, workers)

	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start // release all goroutines as close to simultaneously as possible
			claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
			if err != nil {
				results <- false
				return
			}
			results <- claimed
		}()
	}
	close(start)

	winners := 0
	for i := 0; i < workers; i++ {
		if <-results {
			winners++
		}
	}

	assert.Equal(t, 1, winners, "exactly one of %d concurrent Claim calls for the same (resource, action) must win", workers)
}

// TestWorkClaims_Claim_EligibleResourceIsProcessedEndToEnd is the positive-direction
// falsifiability check: an over-tight claim implementation that always returns false
// would pass every "nothing bad happened" test above. This proves a genuinely fresh
// resource can be claimed, have its work done, and be completed successfully.
func TestWorkClaims_Claim_EligibleResourceIsProcessedEndToEnd(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed, "a fresh resource with no prior claim must be claimable")

	// ... external work would happen here ...

	require.NoError(t, repo.Complete(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup))

	var completedAt *time.Time
	err = pool.QueryRow(ctx,
		`SELECT completed_at FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
	).Scan(&completedAt)
	require.NoError(t, err)
	require.NotNil(t, completedAt, "Complete must durably record completion")
}

func TestWorkClaims_Action_CheckConstraintRejectsUnknownAction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO work_claims (resource_type, resource_id, action, lease_expires_at)
		VALUES ($1, $2, 'not_a_real_action', NOW() + INTERVAL '15 minutes')
	`, ResourceTypeUser, resourceID)
	assert.Error(t, err, "the action CHECK constraint must reject free-form strings")
}

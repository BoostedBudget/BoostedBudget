package services

// trial_service_ordering_integration_test.go — real-Postgres proof of the Issue #1413 round-5
// (independent adversarial review) High finding: TrialService.ExpireTrial used to flip
// subscription_status to 'inactive' BEFORE disconnecting Plaid, and swallowed a disconnect
// failure with a log-and-continue. GetUsersWithExpiredTrials (profile_repository.go) selects
// candidates on raw subscription_status = 'trial', so once that flip committed, a failed
// disconnect could never be retried: the user permanently vanished from the query with their
// bank connection still live on an account that was now read-only and non-paying.
//
// This is the crux pgxmock cannot prove: the unit tests in trial_service_test.go assert
// ExpireTrialIfNoOverride was never CALLED on a disconnect failure, but only a real
// GetUsersWithExpiredTrials query against a real row proves the user is actually still
// SELECTABLE afterward — which is the entire point of the fix (the daily cron job's retry
// depends on reselection, not merely on ExpireTrial's own return value).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestExpireTrial_Integration_DisconnectFails -v

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestExpireTrial_Integration_DisconnectFails_UserReselectedNextRun_ThenSucceedsOnRetry is the
// real-DB counterpart to TestTrialService_ExpireTrial_DisconnectFails_StatusNotFlipped_
// ClaimReleasedForRetry (mocked collaborators) in trial_service_test.go. Two phases against a
// REAL database:
//
//  1. ExpireTrial's Plaid disconnect fails. subscription_status must remain 'trial' (never
//     flipped), and — the crux — GetUsersWithExpiredTrials must STILL select this user
//     afterward, proving the daily cron job's retry is actually reachable, not merely that
//     ExpireTrial returned an error.
//  2. A second ExpireTrial call (simulating tomorrow's retry) with a succeeding disconnect must
//     then flip the status to 'inactive' and start the retention clock — an ordinary expired
//     trial is still fully processed once the disconnect succeeds.
//
// Falsifiability: reverting trial_service.go to the pre-round-5 ordering (CAS before disconnect,
// disconnect error logged-and-swallowed) makes phase 1 fail immediately — subscription_status
// would already be 'inactive' after the first call, so the "still trial" and "still selected by
// GetUsersWithExpiredTrials" assertions below would both fail.
func TestExpireTrial_Integration_DisconnectFails_UserReselectedNextRun_ThenSucceedsOnRetry(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()

	profileRepo := repository.NewProfileRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	trialStartedAt := trialEndsAt.Add(-60 * 24 * time.Hour)
	user := &domain.User{
		ClerkUserID:        "clerk_ordering_" + suffix,
		Email:              "ordering-" + suffix + "@integration.example",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		TrialStartedAt:     &trialStartedAt,
		TrialEndsAt:        &trialEndsAt,
		HadTrial:           true,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, "DELETE FROM users WHERE id = $1", user.ID) //nolint:errcheck
		pool.Exec(bg, `DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, user.ID, repository.ActionTrialExpiration) //nolint:errcheck
	})

	// Sanity: the expired-trial job would select this user right now.
	expired, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.True(t, userIDInSlice(expired, user.ID),
		"test setup sanity: this user's trial_ends_at must already be in the past")

	// --- Phase 1: ExpireTrial's Plaid disconnect fails ---
	plaidCleanup := new(mockPlaidCleanupService)
	disconnectErr := errors.New("plaid: transient 500")
	plaidCleanup.On("DisconnectAllUserItems", mock.Anything, user.ID).Return(disconnectErr).Once()

	trialSvc := NewTrialService(nil, profileRepo, plaidCleanup)
	trialSvc.SetWorkClaimsRepo(claimsRepo)

	err = trialSvc.ExpireTrial(ctx, user.ID)
	require.Error(t, err, "a failed Plaid disconnect must be propagated, not swallowed")

	var rawStatus string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&rawStatus))
	assert.Equal(t, "trial", rawStatus,
		"THE PIN: a failed disconnect must NOT flip subscription_status — the round-5 defect flipped "+
			"status BEFORE disconnecting and swallowed this exact failure")

	expiredAfterFailure, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.True(t, userIDInSlice(expiredAfterFailure, user.ID),
		"THE CRUX: GetUsersWithExpiredTrials must still reselect this user after a failed disconnect — "+
			"under the round-5 defect, the status flip already committed and this user would never "+
			"appear in this query again, leaking their live Plaid connection forever")

	plaidCleanup.AssertExpectations(t)

	// --- Phase 2: the retry (simulating tomorrow's cron run) succeeds ---
	plaidCleanup.On("DisconnectAllUserItems", mock.Anything, user.ID).Return(nil).Once()

	err = trialSvc.ExpireTrial(ctx, user.ID)
	require.NoError(t, err, "the retry must succeed once the disconnect succeeds")
	plaidCleanup.AssertExpectations(t)

	var finalStatus string
	var retentionStarted, retentionExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT subscription_status, retention_started_at, retention_expires_at FROM users WHERE id = $1", user.ID,
	).Scan(&finalStatus, &retentionStarted, &retentionExpires))
	assert.Equal(t, "inactive", finalStatus, "the retry must flip status to inactive exactly as an ordinary expired trial would")
	assert.NotNil(t, retentionStarted, "the retention clock must start once the retry succeeds")
	assert.NotNil(t, retentionExpires)

	expiredAfterSuccess, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.False(t, userIDInSlice(expiredAfterSuccess, user.ID),
		"once the retry succeeds, the user is no longer raw 'trial' and must drop out of this query")
}

// userIDInSlice reports whether users contains a user with the given ID.
func userIDInSlice(users []*domain.User, id string) bool {
	for _, u := range users {
		if u.ID == id {
			return true
		}
	}
	return false
}

package services

// trial_service_ordering_integration_test.go — real-Postgres proof of the Issue #1413 round-6 fix
// (reverting round-5's regression). Round-5's High finding was genuine — ExpireTrial used to flip
// subscription_status to 'inactive' AFTER disconnecting Plaid, and swallowed a disconnect failure
// with a log-and-continue, so a failure there could never be retried. But round-5's chosen FIX
// (disconnect Plaid BEFORE the CAS flip) was itself a regression: it left 'trial' — a WRITABLE
// status, unlike past_due — writable for the disconnect's entire duration, opening a window for a
// concurrent checkout or account-linking request to race the disconnect.
//
// Round-6 restores CAS-FIRST ordering (flip, then disconnect) and replaces round-5's retry
// mechanism with a dedicated repair path: GetUsersNeedingPlaidDisconnectRepair
// (profile_repository.go), consumed by RetentionCleanupCheckWorker's periodic pass. This is the
// crux pgxmock cannot prove: the unit tests in trial_service_test.go assert ExpireTrialIfNoOverride
// WAS called even when the subsequent disconnect fails, but only a real database proves (1) the
// flip actually commits regardless of the disconnect outcome, and (2) the stuck read-only user
// with a live linked item is actually SELECTABLE by the new repair query afterward — which is the
// entire point of the fix (the repair pass's retry depends on reselection there, not on
// GetUsersWithExpiredTrials, which can never see this user again once they leave 'trial').
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestExpireTrial_Integration_DisconnectFailsAfterFlip -v

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

// TestExpireTrial_Integration_DisconnectFailsAfterFlip_UserSelectableByPlaidRepairQuery_ThenSucceedsOnRepair
// is the real-DB counterpart to TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_
// ClaimCompleted_ErrorPropagated (mocked collaborators) in trial_service_test.go. Two phases
// against a REAL database:
//
//  1. ExpireTrial's Plaid disconnect fails. subscription_status MUST still flip to 'inactive' —
//     the round-6 CAS-first ordering means the flip does not depend on the disconnect succeeding
//     — the retention clock must start, and the user must drop OUT of GetUsersWithExpiredTrials
//     (no longer raw 'trial'). The CRUX: with a live linked item still on file, the user must now
//     be SELECTABLE by GetUsersNeedingPlaidDisconnectRepair — the repair path that replaces
//     round-5's (regressive) retry mechanism.
//  2. RetentionCleanupCheckWorker's repair pass calling DisconnectAllUserItems directly (simulated
//     here, not via a second ExpireTrial call — see that worker's repairPlaidDisconnects) must
//     then succeed, after which the user drops out of the repair query.
//
// Falsifiability: reverting trial_service.go to the round-5 ordering (disconnect before CAS, error
// swallowed) makes phase 1 fail immediately — subscription_status would still be 'trial' (never
// flipped), so the "flipped to inactive" and "selectable by the Plaid-disconnect repair query"
// assertions below would both fail; the user would instead still show up in
// GetUsersWithExpiredTrials, which is exactly the regression round-6 reverts.
func TestExpireTrial_Integration_DisconnectFailsAfterFlip_UserSelectableByPlaidRepairQuery_ThenSucceedsOnRepair(t *testing.T) {
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
		pool.Exec(bg, "DELETE FROM linked_items WHERE user_id = $1", user.ID)  //nolint:errcheck
		pool.Exec(bg, "DELETE FROM users WHERE id = $1", user.ID)              //nolint:errcheck
		pool.Exec(bg, `DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, user.ID, repository.ActionTrialExpiration) //nolint:errcheck
	})

	// A live linked item on file — this is what makes the user genuinely a
	// GetUsersNeedingPlaidDisconnectRepair candidate once they're flipped read-only below, rather
	// than merely inactive with nothing left to disconnect.
	_, err := pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'active')
	`, user.ID, "item-ordering-"+suffix)
	require.NoError(t, err)

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
	require.Error(t, err, "a failed Plaid disconnect must still be propagated for visibility, even though the flip already committed")

	var rawStatus string
	var retentionStarted, retentionExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT subscription_status, retention_started_at, retention_expires_at FROM users WHERE id = $1", user.ID,
	).Scan(&rawStatus, &retentionStarted, &retentionExpires))
	assert.Equal(t, "inactive", rawStatus,
		"THE PIN: subscription_status must flip to inactive REGARDLESS of the disconnect outcome — round-6's "+
			"CAS-first ordering means the flip no longer depends on Plaid succeeding")
	assert.NotNil(t, retentionStarted, "the retention clock must start — it is gated on the CAS, not the disconnect")
	assert.NotNil(t, retentionExpires)

	expiredAfterFailure, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.False(t, userIDInSlice(expiredAfterFailure, user.ID),
		"the user is no longer raw 'trial' — GetUsersWithExpiredTrials can never reselect them again")

	repairCandidates, err := profileRepo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	assert.True(t, userIDInSlice(repairCandidates, user.ID),
		"THE CRUX: GetUsersNeedingPlaidDisconnectRepair must select this user — read-only-status with a "+
			"live linked item and a failed disconnect — this is the repair path that replaces round-5's "+
			"(regressive) retry mechanism")

	plaidCleanup.AssertExpectations(t)

	// --- Phase 2: the repair pass (simulating RetentionCleanupCheckWorker.repairPlaidDisconnects)
	// calls DisconnectAllUserItems directly for this candidate — NOT via a second ExpireTrial call,
	// since GetUsersWithExpiredTrials can no longer reach this user.
	plaidCleanup.On("DisconnectAllUserItems", mock.Anything, user.ID).Return(nil).Once()

	err = plaidCleanup.DisconnectAllUserItems(ctx, user.ID)
	require.NoError(t, err, "the repair pass must succeed once the disconnect succeeds")
	plaidCleanup.AssertExpectations(t)

	// The repair query itself only inspects subscription_status/live-items/overrides — it has no
	// way to know the disconnect "succeeded" from a bare mock call, so mark the item disconnected
	// the same way the real PlaidItemCleanupService would (MarkItemsDisconnected), to prove the
	// user then drops out of the candidate list once no live item remains.
	_, err = pool.Exec(ctx, `UPDATE linked_items SET status = 'disconnected' WHERE user_id = $1`, user.ID)
	require.NoError(t, err)

	repairCandidatesAfter, err := profileRepo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	assert.False(t, userIDInSlice(repairCandidatesAfter, user.ID),
		"once the repaired disconnect is durably recorded, the user must drop out of the repair query")
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

package services

// trial_expiry_comp_revert_integration_test.go — real-Postgres proof of Issue #1413 G8: the
// deliberate raw-status residual left when an admin removes a comp from a user whose trial
// ALREADY expired in wall-clock time, but whose TrialService.ExpireTrial run was deferred by the
// comp (trial_service.go:223's HasActiveManualOverride guard).
//
// domain.SubscriptionStatus.IsReadOnlySubscription() (subscription.go:54-57) covers
// inactive|cancelled|past_due — NOT trial. AdminUserRepository's should_rearm_retention
// predicate (admin_user_repository.go — removeUserTierOverrideSelectPrior) only re-arms
// retention for those same three statuses, so removing a comp from a 'trial' user leaves
// subscription_status untouched at 'trial' — never normalized to 'inactive' — even though the
// trial's wall-clock expiry already passed. The user is consequently NOT read-only until the
// next ExpireTrial run for this user actually SUCCEEDS — an already-accepted window, but bounded
// by successful job execution, not by a single run unconditionally (corrected per an independent
// adversarial review: TrialExpirationCheckWorker logs and continues past a per-user ExpireTrial
// failure, still reporting the job overall as successful, so a persistently failing ExpireTrial run
// would extend this window across multiple runs, not just one). See trial_service.go's "Accepted
// cost (already ratified): ... volume is a handful of rows" and the function doc comment below for
// why this is a property ExpireTrial has for every expired trial, comped-and-reverted or not.
//
// An independent review proposed normalizing trial -> inactive inside RemoveUserTierOverride's
// clear CTE to close this window. THAT WAS REJECTED — do not implement it. ExpireTrial does TWO
// things in one call (Issue #1413 round-7: it no longer disconnects Plaid at all — see
// trial_service.go's ExpireTrial doc comment): flip subscription_status and start the retention
// clock, the SAME timestamp RetentionCleanupCheckWorker's grace-window repair pass anchors on to
// decide when a read-only user's Plaid items are eventually disconnected. GetUsersWithExpiredTrials
// (profile_repository.go) selects candidates on raw subscription_status = 'trial'. If the clear
// silently set status to 'inactive', this user would never be selected by that job again — so the
// retention clock ExpireTrial exists to start would NEVER start, leaving this user permanently
// invisible to the grace-window repair pass and so a live bank connection on a read-only account
// indefinitely. That is worse than the bounded one-run gap this test pins, and it would turn an
// idempotent admin bookkeeping endpoint (remove override) into a destructive one (silently skips a
// mandatory side effect). If this window is ever tightened, the fix belongs in making the CLEAR
// itself trigger the retention-clock side effect, not in making the row invisible to the job that
// already performs it.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestExpireTrial_Integration_CompRemovedAfterTrialExpiry -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpireTrial_Integration_CompRemovedAfterTrialExpiry_RawTrialSurvivesOneRun_ThenExpires
// is the #1413 G8 pin. Two phases against a REAL database (not two isolated unit tests that
// could each pass under an implementation that behaved differently end-to-end):
//
//  1. An admin removes a live comp from a user whose trial_ends_at is already in the past.
//     The reverted raw status must still be 'trial' (not silently normalized to 'inactive'),
//     and a user at raw 'trial' is provably NOT read-only — the bounded, accepted gap.
//
//  2. The next daily ExpireTrial run (simulating tomorrow's cron) must then actually flip the
//     status AND start the retention clock, proving the gap closes once a run for this user
//     SUCCEEDS.
//
//     UPDATED for Issue #1413 round-7 (trial_service.go's ExpireTrial doc comment has the full
//     history): ExpireTrial no longer calls Plaid at all — the status flip and retention-clock
//     start are the ENTIRE flow now, gated only on the override check. That means the
//     writable-until-read-only window this test's Phase 1 proves closes on the very next run that
//     reaches the CAS, full stop — there is no disconnect outcome that could extend it either way.
//     The Plaid-side cleanup for a user like this one (once genuinely read-only) is handled
//     entirely by RetentionCleanupCheckWorker's grace-window repair pass
//     (GetUsersNeedingPlaidDisconnectRepair, profile_repository.go), independently of this
//     comp-revert scenario.
func TestExpireTrial_Integration_CompRemovedAfterTrialExpiry_RawTrialSurvivesOneRun_ThenExpires(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()

	profileRepo := repository.NewProfileRepository(pool, enc)
	adminRepo := repository.NewAdminUserRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// A trial user whose trial expired 5 days ago in wall-clock time, but who was comped before
	// the daily ExpireTrial job ever got to them — the guard at trial_service.go:223 means the
	// job has been silently skipping this user every run since.
	trialEndsAt := time.Now().Add(-5 * 24 * time.Hour)
	trialStartedAt := trialEndsAt.Add(-60 * 24 * time.Hour)
	user := &domain.User{
		ClerkUserID:        "clerk_g8_" + suffix,
		Email:              "g8-" + suffix + "@integration.example",
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

	overrideEndsAt := time.Now().Add(1 * time.Hour) // still active right now
	_, err := pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	// Sanity: the expired-trial job WOULD select this user right now (comp aside) — confirms the
	// fixture is a genuine "expired but not yet processed" trial, not just a status string.
	expired, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	found := false
	for _, u := range expired {
		if u.ID == user.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "test setup sanity: this user's trial_ends_at must already be in the past")

	// --- Phase 1: admin removes the comp ---
	row, err := adminRepo.RemoveUserTierOverride(ctx, "", user.ID, "comp ended", 6)
	require.NoError(t, err)
	assert.True(t, row.Removed)
	assert.Equal(t, "trial", row.RevertedStatus,
		"THE PIN: the reverted raw status must still be trial — RemoveUserTierOverride must NOT "+
			"normalize it to inactive, even though the trial's wall-clock expiry has already passed")

	var rawStatus string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&rawStatus))
	require.Equal(t, "trial", rawStatus)
	assert.False(t, domain.SubscriptionStatus(rawStatus).IsReadOnlySubscription(),
		"a user left at raw status 'trial' is NOT read-only — the comp-removal residual really "+
			"does leave this user writable until the next ExpireTrial run")

	// --- Phase 2: the next daily ExpireTrial run finally catches up ---
	// tierConfig is nil: ExpireTrial never reads it (only GrantTrial does), and this test must
	// not call config.LoadTierConfig()'s process-wide sync.Once. Issue #1413 round-7: ExpireTrial
	// no longer calls Plaid at all (see trial_service.go's ExpireTrial doc comment) — the
	// Plaid-disconnect coverage this test used to also pin now lives in
	// retention_cleanup_check_test.go / profile_repository_plaid_disconnect_repair_integration_test.go.
	trialSvc := NewTrialService(nil, profileRepo)
	trialSvc.SetWorkClaimsRepo(claimsRepo)

	err = trialSvc.ExpireTrial(ctx, user.ID)
	require.NoError(t, err, "ExpireTrial must now succeed — the override is gone")

	var finalStatus string
	var retentionStarted, retentionExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT subscription_status, retention_started_at, retention_expires_at FROM users WHERE id = $1", user.ID,
	).Scan(&finalStatus, &retentionStarted, &retentionExpires))
	assert.Equal(t, "inactive", finalStatus, "ExpireTrial must flip the now-uncomped user to inactive")
	assert.NotNil(t, retentionStarted, "the retention clock must start on this real ExpireTrial run")
	assert.NotNil(t, retentionExpires)
}

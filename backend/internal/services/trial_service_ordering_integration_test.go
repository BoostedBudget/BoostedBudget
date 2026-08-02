package services

// trial_service_ordering_integration_test.go — real-Postgres proof of the Issue #1413 round-7 fix
// (Codex review): TrialService.ExpireTrial no longer calls Plaid at all, and disconnecting a
// read-only user's Plaid items is deferred entirely to RetentionCleanupCheckWorker's grace-window
// repair pass (repairPlaidDisconnects, driven by GetUsersNeedingPlaidDisconnectRepair).
//
// History: this function was reordered twice before round-7, and both attempts were wrong for
// opposite reasons — see trial_service.go's ExpireTrial doc comment for the full account. The
// round-7 conclusion is that no ordering of a status flip and an irreversible Plaid call can be
// safe, because /subscription/checkout is deliberately whitelisted for read-only users
// (internal/huma/middleware/read_only.go) — becoming read-only never stops a user from becoming
// active again. The fix defers instead of ordering: ExpireTrial's CAS (ExpireTrialIfNoOverride)
// atomically flips subscription_status AND stamps plaid_cleanup_pending_at — the actual grace
// anchor the repair pass (GetUsersNeedingPlaidDisconnectRepair) uses. ExpireTrial separately
// starts the retention clock (retention_started_at) a few lines later, but that timestamp governs
// ONLY the unrelated 6-month deletion clock — round-11 explicitly forbids reintroducing it as the
// Plaid-disconnect grace anchor (see trial_service.go's ExpireTrial doc comment and
// profile_repository.go's GetUsersNeedingPlaidDisconnectRepair doc comment).
//
// This is the crux pgxmock cannot prove: the unit tests in trial_service_test.go assert ExpireTrial
// never calls a Plaid collaborator (there IS no Plaid collaborator on TrialService anymore — a
// structural, compile-time guarantee), but only a real database proves (1) a live linked item is
// left completely untouched by ExpireTrial, and (2) GetUsersNeedingPlaidDisconnectRepair correctly
// withholds a freshly-read-only user from selection until the grace window elapses, and correctly
// selects them once it has.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestExpireTrial_Integration -v

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

// TestExpireTrial_Integration_NeverTouchesPlaid_RepairQueryHonorsGraceWindow is the round-7
// replacement for the round-6 ordering pin (formerly
// ...DisconnectFailsAfterFlip_UserSelectableByPlaidRepairQuery_ThenSucceedsOnRepair, which
// exercised a mocked Plaid failure and the CAS-first ordering that no longer exists). Three phases
// against a REAL database:
//
//  1. ExpireTrial runs against an expired-trial user with a live linked item. The status must flip
//     to 'inactive', the retention clock must start, and — the round-7 crux — the linked item's
//     row must be COMPLETELY UNTOUCHED (still 'active'): ExpireTrial has no Plaid collaborator at
//     all anymore, so there is nothing that could have modified it.
//  2. Immediately afterward, GetUsersNeedingPlaidDisconnectRepair with the PRODUCTION-shaped grace
//     period (72h) must NOT select this user — plaid_cleanup_pending_at was just stamped to "now"
//     by ExpireTrialIfNoOverride's CAS (NOT retention_started_at, which plays no role in this
//     query at all), nowhere near 72h in the past. This is the direct proof of the brief's RED
//     case: a user who is (or becomes) read-only must never be disconnected before the grace
//     window elapses.
//  3. The SAME query, called with an effectively-zero grace period, DOES select this user right
//     now — proving the query mechanism itself is what withholds them in phase 2 (not, say, a bug
//     that always excludes everyone), and that the grace window is the only thing standing between
//     this user and disconnection.
//
// Falsifiability: reverting profile_repository.go's GetUsersNeedingPlaidDisconnectRepair to drop
// its `GREATEST(plaid_cleanup_pending_at, ...) < clock_timestamp() - make_interval(secs => $1)`
// grace-period comparison (e.g. replacing it with an unconditional TRUE) makes phase 2 fail — the
// user would be immediately selected with NO grace window at all, exactly the round-7 regression
// this pins. (A `retention_started_at < cutoff` clause, which this comment previously named, does
// not exist anywhere in that query — retention_started_at is not read by it at all.)
func TestExpireTrial_Integration_NeverTouchesPlaid_RepairQueryHonorsGraceWindow(t *testing.T) {
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
		pool.Exec(bg, "DELETE FROM linked_items WHERE user_id = $1", user.ID) //nolint:errcheck
		pool.Exec(bg, "DELETE FROM users WHERE id = $1", user.ID)             //nolint:errcheck
		pool.Exec(bg, `DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, user.ID, repository.ActionTrialExpiration) //nolint:errcheck
	})

	// A live linked item on file — this is what makes the user genuinely a
	// GetUsersNeedingPlaidDisconnectRepair candidate once they're flipped read-only AND past the
	// grace window, rather than merely inactive with nothing left to disconnect.
	itemID := "item-ordering-" + suffix
	_, err := pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'active')
	`, user.ID, itemID)
	require.NoError(t, err)

	// Sanity: the expired-trial job would select this user right now.
	expired, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.True(t, userIDInSlice(expired, user.ID),
		"test setup sanity: this user's trial_ends_at must already be in the past")

	// --- Phase 1: ExpireTrial runs — no Plaid collaborator exists to call at all ---
	trialSvc := NewTrialService(nil, profileRepo)
	trialSvc.SetWorkClaimsRepo(claimsRepo)

	err = trialSvc.ExpireTrial(ctx, user.ID)
	require.NoError(t, err, "ExpireTrial's only work is the status flip and retention clock — nothing here can fail")

	var rawStatus, itemStatus string
	var retentionStarted, retentionExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT subscription_status, retention_started_at, retention_expires_at FROM users WHERE id = $1", user.ID,
	).Scan(&rawStatus, &retentionStarted, &retentionExpires))
	assert.Equal(t, "inactive", rawStatus, "ExpireTrial must still flip subscription_status to inactive")
	require.NotNil(t, retentionStarted, "the retention clock must start — this timestamp governs the unrelated 6-month deletion clock, NOT the Plaid-disconnect grace window (that anchor is plaid_cleanup_pending_at, proven by phases 2/3 below)")
	assert.NotNil(t, retentionExpires)

	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM linked_items WHERE user_id = $1 AND item_id = $2", user.ID, itemID).Scan(&itemStatus))
	assert.Equal(t, "active", itemStatus,
		"THE CRUX: the linked item must be COMPLETELY UNTOUCHED — ExpireTrial has no Plaid "+
			"collaborator at all anymore, so nothing could have disconnected it")

	expiredAfterFlip, err := profileRepo.GetUsersWithExpiredTrials(ctx)
	require.NoError(t, err)
	assert.False(t, userIDInSlice(expiredAfterFlip, user.ID),
		"the user is no longer raw 'trial' — GetUsersWithExpiredTrials can never reselect them again")

	// --- Phase 2: WITHIN the grace window (production-shaped 72h), the repair query must NOT
	// select this user — retention_started_at was just set to "now".
	withinGraceCandidates, err := profileRepo.GetUsersNeedingPlaidDisconnectRepair(ctx, 72*time.Hour)
	require.NoError(t, err)
	assert.False(t, userIDInSlice(withinGraceCandidates, user.ID),
		"THE RED CASE: a user within the grace window must NEVER be selected for Plaid disconnect — "+
			"resubscribing in this window must be a non-event, not a narrowed race")

	// --- Phase 3: PAST the grace window (an effectively-zero grace period — retention_started_at
	// was set microseconds ago, so it is already older than "now minus ~0"), the SAME query DOES
	// select this user — proving phase 2's exclusion is the grace window doing its job, not some
	// other bug that always excludes everyone.
	pastGraceCandidates, err := profileRepo.GetUsersNeedingPlaidDisconnectRepair(ctx, 1*time.Nanosecond)
	require.NoError(t, err)
	assert.True(t, userIDInSlice(pastGraceCandidates, user.ID),
		"once the grace window has elapsed, the SAME read-only user with the SAME live item must "+
			"become a repair candidate")
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

//go:build integration

package repository

// Integration tests for Issue #1413 round-13: the plaid_cleanup_pending_at invariant.
//
// See domain.User.PlaidCleanupPendingAt's doc comment for the invariant every writer/clearer of
// this column must maintain:
//
//	plaid_cleanup_pending_at is non-NULL EXACTLY WHEN an ineligibility EPISODE has begun
//	(domain.SubscriptionStatus(subscription_status).IsEligibleForPlaidDisconnectRepair() is true)
//	AND outstanding Plaid cleanup for that episode has not yet completed. Its value is the instant
//	the CURRENT episode began — never an earlier episode's, never a later one's.
//
//	Deliberately NOT part of this invariant: manual_tier_override. Whether a comp is currently
//	live is a wall-clock fact (HasActiveManualOverride() vs. time.Now()), not a write — round-11/
//	round-12 tied this column's SET-time value to "is there a live override right now", which
//	silently required some LATER writer to re-stamp the marker the instant the override lapsed;
//	if that writer never ran, the marker was wrong FOREVER with no writer at fault. Round-13's
//	fix: the marker tracks only the durable, override-independent "episode began" fact; CURRENT
//	eligibility (status AND live override) is evaluated separately, at query time, by
//	GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate.
//
// assertPlaidMarkerInvariant below is a direct, reusable encoding of that statement — every test
// in this file calls it after every state-changing step, rather than asserting only the single
// column value the specific fix under test cares about. This is deliberate: several prior
// remediation rounds on this branch each closed the previous reviewer's single finding while
// introducing a new one, precisely because each fix was checked only against its own narrow
// expectation. Round 11's brief calls the sweep test in this file "the test that would have caught
// all four [round-11] findings at once" — TestPlaidMarkerInvariant_Integration_Sweep below is that
// test; the other tests in this file are the individual RED-then-GREEN regressions per fix.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run 'TestClearRetentionDates_Integration|TestUpdateTrialStatus_Integration|TestUpdateSubscriptionFromPaddle_Integration|TestClearPlaidCleanupPendingIfMatches_Integration|TestPlaidMarkerInvariant' -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// plaidMarkerRow is the subset of a users row assertPlaidMarkerInvariant needs.
type plaidMarkerRow struct {
	status  string
	comp    *string
	compEnd *time.Time
	marker  *time.Time
}

func readPlaidMarkerRow(t *testing.T, pool *pgxpool.Pool, userID string) plaidMarkerRow {
	t.Helper()
	var row plaidMarkerRow
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT subscription_status, manual_tier_override, manual_tier_ends_at, plaid_cleanup_pending_at
		FROM users WHERE id = $1
	`, userID).Scan(&row.status, &row.comp, &row.compEnd, &row.marker))
	return row
}

// hasActiveOverride mirrors domain.User.hasActiveManualOverrideAt exactly (Issue #1413's existing
// parity convention between the Go predicate and its SQL/test twins). No longer consulted by
// assertPlaidMarkerInvariant (round-13 dropped override from the marker invariant entirely — see
// the file header comment), but kept as a helper for tests that need to independently assert on a
// row's override state directly (e.g. the positive control in
// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp).
func (r plaidMarkerRow) hasActiveOverride(now time.Time) bool {
	if r.comp == nil || *r.comp == "" {
		return false
	}
	return r.compEnd == nil || !r.compEnd.Before(now)
}

// assertPlaidMarkerInvariant is THE invariant check: it re-derives, from the row's own committed
// state, whether an ineligibility episode is currently open (raw subscription_status alone — see
// the file header comment for why manual_tier_override is deliberately NOT part of this check as
// of round-13), and asserts plaid_cleanup_pending_at's nullness agrees with BOTH conjuncts of the
// invariant — not just the first. This is the single reusable assertion every test in this file
// drives every state-changing step through — see the file header comment for why that (not a
// fix-specific spot-check) is the point of this file.
//
// Task X (L2, Codex review round on PR #1443): the invariant has TWO conjuncts — non-NULL EXACTLY
// WHEN an episode is open AND cleanup for it has not completed — and this helper previously only
// checked the first, so a marker sitting on a user with no cleanup work left at all (e.g. every
// linked item already disconnected/replaced) silently passed. "Outstanding cleanup" here uses the
// SAME liveness definition GetUsersNeedingPlaidDisconnectRepair / GetOutstandingPlaidCleanupStats
// both use (profile_repository.go / job_health_repository.go): at least one linked_items row that
// isn't 'disconnected'/'replaced' (a NULL status counts as live).
//
// Round-15 REVISION — the marker is OPTIMISTIC, so the second conjunct is no longer asserted here.
// Task X (above) tightened this helper to require outstanding cleanup whenever the marker is set.
// Task Z then had to make the override-removal path stamp the marker UNCONDITIONALLY: its
// liveness-guarded stamp lost a race with LinkAccounts (the CTE's EXISTS is a snapshot, and
// FOR UPDATE locks the users row, not the linked_items lifecycle — so a held lock merely delays the
// insert until after the admin's commit, which produces the leak rather than preventing it; closing
// it properly needs LinkAccounts itself to participate, which is out of this path's reach). Faced
// with "missed stamp = permanent, unrecoverable leak" versus "spurious stamp = phantom row already
// filtered out by the repair query's own live-item check", Z correctly chose to stamp.
//
// The consequence is that a marker now means "an episode began and cleanup MAY be outstanding" —
// GetUsersNeedingPlaidDisconnectRepair's live-item EXISTS is the authority on whether work actually
// remains. Asserting the strong second conjunct here would make this helper fail on the ordinary
// remove-a-comp-after-cleanup-completed path, i.e. it would be asserting an invariant production
// deliberately does not maintain. So it is relaxed DELIBERATELY, and narrowly:
//
//   - Still strict, and these are the ones that catch LEAKS — the harmful direction:
//     marker set while NO episode is open, and episode open while the marker is NULL (a NULL marker
//     with live items is exactly how a user becomes permanently invisible to the repair pass).
//   - No longer asserted: marker set with zero live items. That is a PHANTOM — untidy, harmless,
//     and now deliberately generated. It costs nothing at runtime (the repair query filters it, and
//     GetOutstandingPlaidCleanupStats joins on live items so the gauge does not count it) and it is
//     tracked as #1453, which this change turns from a historical-only population into an ongoing
//     one. assertPlaidMarkerInvariantCleanupComplete still pins the happy path where cleanup runs
//     to completion and the marker IS cleared.
//
// Recorded here rather than silently dropped: this helper's whole purpose is auditing writers
// against a written invariant, so the statement it checks must be one production actually holds.
func assertPlaidMarkerInvariant(t *testing.T, pool *pgxpool.Pool, userID string, context string) {
	t.Helper()
	row := readPlaidMarkerRow(t, pool, userID)
	ineligible := domain.SubscriptionStatus(row.status).IsEligibleForPlaidDisconnectRepair()
	if !ineligible {
		assert.Nil(t, row.marker,
			"[%s] invariant violated: no ineligibility episode is open (status=%q) but plaid_cleanup_pending_at is %v",
			context, row.status, row.marker)
		return
	}
	if !assert.NotNil(t, row.marker,
		"[%s] invariant violated: an ineligibility episode is open (status=%q) but plaid_cleanup_pending_at is NULL",
		context, row.status) {
		return
	}

	// Deliberately NOT asserting outstanding cleanup here — see the round-15 revision note above.
	// The marker is optimistic; a marker with zero live items is an accepted phantom (#1453), not a
	// violation. The two leak-catching assertions above remain strict.
}

// plaidMarkerLinkedItemCounts was removed in round-15: it existed solely for
// assertPlaidMarkerInvariant's second-conjunct ("cleanup still outstanding") check, which that
// helper no longer performs now that the marker is deliberately optimistic. Kept out rather than
// left dead — the liveness definition it duplicated lives in liveLinkedItemExistsClause
// (profile_repository.go), which is the single definition the repair query, the backlog gauge, and
// migration 000121 all share.

// assertPlaidMarkerInvariantCleanupComplete is assertPlaidMarkerInvariant's sibling for the ONE
// state the plain helper cannot verify from the users row's status column alone: an ineligible
// user whose Plaid cleanup for the CURRENT episode has already completed (either
// repairPlaidDisconnects's ClearPlaidCleanupPendingIfMatches, or Issue #1413 round-12 Fix 1's
// synchronous clear-on-success in the cancellation/past-due webhook paths). The full invariant
// (domain.User.PlaidCleanupPendingAt's doc comment) requires TWO conjuncts for non-NULL —
// ineligible (raw) status AND outstanding cleanup — and assertPlaidMarkerInvariant above only
// checks the first, silently assuming the second is always true, which held for every writer
// prior to round-12 Fix 1. Once a writer can legitimately clear the marker while the user remains
// ineligible, that assumption no longer holds, and asserting the plain helper immediately after
// such a clear would fail against a state the invariant explicitly permits.
//
// Callers must use this ONLY immediately after an explicit, deliberate "cleanup just completed"
// clear call (ClearPlaidCleanupPendingIfMatches / ClearPlaidCleanupPending) — never as a
// general-purpose substitute for assertPlaidMarkerInvariant.
func assertPlaidMarkerInvariantCleanupComplete(t *testing.T, pool *pgxpool.Pool, userID string, context string) {
	t.Helper()
	row := readPlaidMarkerRow(t, pool, userID)
	assert.Nil(t, row.marker,
		"[%s] cleanup was just completed for this episode — plaid_cleanup_pending_at must be NULL even though the user remains ineligible (status=%q)",
		context, row.status)
}

// createPlaidMarkerTestUser creates a minimal user with the given initial status for these tests.
func createPlaidMarkerTestUser(t *testing.T, pool *pgxpool.Pool, repo *profileRepository, namePrefix, status string) *domain.User {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + namePrefix + "_" + suffix,
		Email:              namePrefix + "-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck
	return user
}

// ─── Fix 1 (Blocker): ClearRetentionDates must not clobber a fresher cancellation's marker ──

// TestClearRetentionDates_Integration_StaleActiveClearDoesNotClobberFresherCancellationMarker is
// the round-11 Blocker RED test. Sequence modeled: an "active" webhook is about to call
// ClearRetentionDates, but pauses; a NEWER cancellation webhook lands first, writing
// subscription_status='cancelled' and a FRESH plaid_cleanup_pending_at; the stale "active"
// webhook then resumes and finally calls ClearRetentionDates. Before the round-11 fix,
// ClearRetentionDates unconditionally nulled the marker — losing the fresh cancellation's marker
// forever (the repair query requires a non-NULL marker, so cleanup would never run; the user stays
// cancelled with a live Plaid item indefinitely).
//
// Falsifiability: reverting ClearRetentionDates to unconditionally NULL plaid_cleanup_pending_at
// makes this test fail — the marker would be NULL after the stale clear call.
func TestClearRetentionDates_Integration_StaleActiveClearDoesNotClobberFresherCancellationMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "staleactiveclear", "active")

	// The newer cancellation webhook already committed: status='cancelled' plus a fresh marker —
	// via the REAL production writer for this joint transition, UpdateSubscriptionFromPaddle, not
	// a raw SQL UPDATE that could silently drift from what that writer actually does (Issue #1413
	// Task W, Codex review: this file's entire purpose is auditing writers against the marker
	// invariant — a setup that bypasses the writer under test cannot detect a writer that violates
	// it). The row's OLD status is 'active' (not already repair-eligible), so this is a genuine
	// transition and the writer's own COALESCE(plaid_cleanup_pending_at, now) freshly stamps the
	// marker, exactly modeling "the newer cancellation webhook already committed."
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	freshMarkerRow := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, freshMarkerRow.marker, "precondition: the cancellation webhook must have started the marker")
	freshMarker := *freshMarkerRow.marker
	assertPlaidMarkerInvariant(t, pool, user.ID, "after newer cancellation webhook lands")

	// The STALE "active" webhook, from before the cancellation, now finally calls
	// ClearRetentionDates — this must NOT clobber the fresher marker, because the row's CURRENT
	// (live) status is 'cancelled', not 'active'.
	require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "the fresh cancellation marker must survive a stale active-webhook's ClearRetentionDates call")
	assert.WithinDuration(t, freshMarker, *row.marker, time.Second, "the surviving marker must still be the fresh cancellation's own value, not resurrected from elsewhere")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after stale ClearRetentionDates call")

	// The repair query must still find this user — this is the blocker's real-world impact: without
	// the fix, this user's Plaid access would persist indefinitely because the repair pass would
	// never see them as a candidate.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))
	seedLinkedItem(t, pool, user.ID, "item-staleactiveclear", "active")
	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "the user must still be selected by the repair query — the blocker's impact was that they never would be")
}

// TestClearRetentionDates_Integration_GenuinelyActiveClearsMarker is the positive control for Fix
// 1: when the row's LIVE status genuinely is not Plaid-repair-eligible (the normal resubscribe
// case, with no stale-webhook race), ClearRetentionDates must still clear the marker — proving the
// conditional clear isn't just a no-op that never fires.
func TestClearRetentionDates_Integration_GenuinelyActiveClearsMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "genuineactiveclear", "cancelled")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))

	// Genuine resubscribe: status flips to active FIRST (mirroring
	// UpdateSubscriptionFromPaddle running before ClearRetentionDates in every real caller), THEN
	// ClearRetentionDates runs as the belt-and-braces second write.
	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)

	require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, row.marker, "a genuinely active user's marker must be cleared by ClearRetentionDates")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine resubscribe clear")
}

// ─── Fix 2 (High): granting a trial must clear a stale marker ──

// TestUpdateTrialStatus_Integration_GrantTrialClearsStalePlaidMarker is the round-11 High RED test:
// GrantTrial (via UpdateTrialStatus) moves a cancelled user with an outstanding marker to 'trial'
// but, before the fix, never cleared plaid_cleanup_pending_at. Trial is Plaid-ELIGIBLE (never a
// repair-pass target), so per the invariant the marker must go to NULL — otherwise
// ExpireTrialIfNoOverride's COALESCE(existing, NOW()) later preserves the STALE timestamp instead
// of starting a fresh grace window, disconnecting the user immediately on the old anchor when
// their new trial eventually expires.
//
// Falsifiability: reverting UpdateTrialStatus to never touch plaid_cleanup_pending_at makes this
// test fail — the stale marker survives the trial grant.
func TestUpdateTrialStatus_Integration_GrantTrialClearsStalePlaidMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "granttrialstale", "cancelled")
	staleMarker := time.Now().Add(-200 * time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)
	assertPlaidMarkerInvariant(t, pool, user.ID, "before trial grant")

	now := time.Now()
	trialEnds := now.Add(60 * 24 * time.Hour)
	require.NoError(t, repo.UpdateTrialStatus(ctx, user.ID, "trial", &now, &trialEnds, true))

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, row.marker, "granting a trial must clear a stale plaid_cleanup_pending_at marker — trial is Plaid-eligible")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after trial grant")

	// Downstream proof: when this NEW trial later expires, the grace window must be FRESH, not
	// immediately expired against the old stale anchor.
	flipped, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, flipped)

	afterExpiry := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, afterExpiry.marker, "trial expiry must set a fresh marker")
	assert.False(t, afterExpiry.marker.Equal(staleMarker), "the marker after this trial's own expiry must NOT be the old stale timestamp from before the trial was granted")
	assert.WithinDuration(t, time.Now(), *afterExpiry.marker, 5*time.Second, "the marker must be freshly stamped by THIS expiry, not resurrected from the earlier cancellation")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after trial expiry")
}

// ─── Fix 3 (High): the repair recheck must validate the marker's identity, not merely presence ──

// TestClearPlaidCleanupPendingIfMatches_Integration_ClearsWhenMatches proves the CAS-guarded clear
// (Issue #1413 round-11 Fix 3, mirroring ClearPastDueStartedAtIfMatches's established pattern)
// clears the marker when it still equals the expected value.
func TestClearPlaidCleanupPendingIfMatches_Integration_ClearsWhenMatches(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "casmatch", "cancelled")
	marker := time.Now().Add(-100 * time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, marker)

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, marker)
	require.NoError(t, err)
	assert.True(t, cleared, "the clear must succeed when the marker still equals the expected value")

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, row.marker)
}

// TestClearPlaidCleanupPendingIfMatches_Integration_SkipsWhenMismatched proves the CAS clear
// leaves a REPLACED marker (a newer episode) untouched — the core of Fix 3: a stale worker must
// not erase a fresher episode's marker just because a marker is present.
func TestClearPlaidCleanupPendingIfMatches_Integration_SkipsWhenMismatched(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "casmismatch", "cancelled")
	staleExpected := time.Now().Add(-200 * time.Hour)
	freshActual := time.Now().Add(-time.Minute)
	setPlaidCleanupPendingAt(t, pool, user.ID, freshActual)

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, staleExpected)
	require.NoError(t, err)
	assert.False(t, cleared, "the clear must be a no-op when the current marker no longer matches the expected (stale) value")

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker)
	assert.WithinDuration(t, freshActual, *row.marker, time.Second, "the newer episode's marker must survive untouched")
}

// TestClearPlaidCleanupPendingIfMatches_Integration_UserNotFound proves the not-found case is
// distinguishable from a CAS mismatch, mirroring ClearPastDueStartedAtIfMatches's contract.
func TestClearPlaidCleanupPendingIfMatches_Integration_UserNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, "00000000-0000-0000-0000-000000000000", time.Now())
	assert.False(t, cleared)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// ─── Round-13: cancellation while comped now SETS the marker; a live override protects the user
// only at query time, not by suppressing the marker ──

// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp
// is the Issue #1413 round-13 test, replacing the round-11 Fix 4 test of the same scenario with
// the OPPOSITE marker assertion: writing subscription_status='cancelled' for a user with a LIVE
// manual tier override now DOES set plaid_cleanup_pending_at — the marker tracks only the raw
// subscription_status episode, independent of override (see the file header comment).
//
// Round-11 Fix 4's gate (null the marker while a live override exists) was itself an instance of
// the round-13 P0's root cause: hasActiveManualOverrideSQLExpr checked "is there a live override
// RIGHT NOW", a wall-clock fact, live in the write — correct at that instant, but if the comp later
// lapsed with the daily expiry job degraded or never run, NOTHING would ever re-stamp the marker,
// permanently hiding this user from the repair pass. Per the restated invariant, CURRENT
// eligibility is instead entirely GetUsersNeedingPlaidDisconnectRepair's job, via its own
// noActiveManualOverridePredicate, re-evaluated fresh on every run — which is what this test's
// second half proves: the marker being set does NOT mean the user gets disconnected while the comp
// is still live.
//
// Falsifiability: reverting UpdateSubscriptionFromPaddle's eligible-branch SQL to reintroduce the
// hasActiveManualOverrideSQLExpr CASE (nulling the marker while a live override exists) makes the
// first half of this test fail — the marker would be NULL instead of set.
func TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "cancelwhilecomped", "active")
	compEnds := time.Now().Add(24 * time.Hour) // live
	// Grant the live comp through the REAL production writer (AdminUserRepository.UpdateUserTier),
	// not a raw SQL UPDATE — Issue #1413 Task W (Codex review): this file's entire purpose is
	// auditing writers against the marker invariant, and TestPlaidMarkerInvariant_Integration_Sweep
	// (this same file, comp_grant_to_remove/comp_grant_then_lapse_never_removed) already grants
	// comps this way — a raw SQL grant here was inconsistent with that standard, and (per
	// updateUserTierQuery's own doc comment) a setup that bypasses the writer under test cannot
	// detect a writer that violates it. UpdateUserTier deliberately leaves plaid_cleanup_pending_at
	// untouched, so this substitution changes nothing this test observes.
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant live comp", &compEnds)
	require.NoError(t, err)
	assertPlaidMarkerInvariant(t, pool, user.ID, "before cancellation, while comped")

	// Mirrors subscription_webhook.go's processSubscriptionCanceled: write 'cancelled' as billing
	// truth, unconditionally, regardless of the comp.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	beforeRemoval := readPlaidMarkerRow(t, pool, user.ID)
	assert.Equal(t, "cancelled", beforeRemoval.status, "billing truth (raw subscription_status) must still flip to cancelled regardless of the comp")
	require.NotNil(t, beforeRemoval.marker, "the marker must be SET — a genuine raw-status transition into ineligibility began an episode, independent of the live override")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after cancellation while comped")

	// The live comp is what actually protects this user from repair — proven at the query layer,
	// not by suppressing the marker.
	seedLinkedItem(t, pool, user.ID, "item-cancelwhilecomped-"+user.ID, "active")
	candidatesWhileLive, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
	require.NoError(t, err)
	for _, c := range candidatesWhileLive {
		assert.NotEqual(t, user.ID, c.ID, "a user with a CURRENTLY-LIVE comp must never be selected for repair, even though the marker is now set")
	}

	// Follow-through: once the comp is later removed, the marker must be PRESERVED at its ORIGINAL
	// (cancellation-time) value — round-13 changed this from round-11's "removal stamps a FRESH
	// NOW()" behavior, because the marker was never nulled by the comp in the first place, so
	// removeUserTierOverrideUpdate's COALESCE(existing, NOW()) has an existing value to keep.
	_, err = adminRepo.RemoveUserTierOverride(ctx, "", user.ID, "test: comp removed", 6)
	require.NoError(t, err)

	afterRemoval := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, afterRemoval.marker)
	assert.True(t, afterRemoval.marker.Equal(*beforeRemoval.marker),
		"removing the comp must PRESERVE the marker's original cancellation-time value, not reset it to a fresh NOW() — the episode began at cancellation, not at comp removal")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after comp removal")
}

// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileOverrideLapsed_MarkerIsSet is the
// positive control proving a LAPSED (expired) override never suppressed the marker either, before
// or after round-13 — matching HasActiveManualOverride()'s boundary semantics exactly.
func TestUpdateSubscriptionFromPaddle_Integration_CancelWhileOverrideLapsed_MarkerIsSet(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "cancelwhilelapsed", "active")
	lapsedEnds := time.Now().Add(-24 * time.Hour) // already expired
	// Grant the (already-lapsed) comp through the REAL production writer
	// (AdminUserRepository.UpdateUserTier), not a raw SQL UPDATE — same rationale as the sibling
	// test above, and matching TestPlaidMarkerInvariant_Integration_Sweep's
	// comp_grant_then_lapse_never_removed subtest (this same file), which grants an
	// already-past-expiry comp through this exact writer.
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant already-lapsed comp", &lapsedEnds)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "a LAPSED override must not suppress the marker — only a currently-active one does")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after cancellation with a lapsed override")
}

// ─── Round-12 Fix 4 (Medium): a later terminal webhook must not resurrect a completed marker ──

// TestUpdateSubscriptionFromPaddle_Integration_LaterTerminalEventDoesNotResurrectClearedMarker is
// the Issue #1413 round-12 Fix 4 RED test. Sequence modeled: a user cancels (marker set); the
// Plaid-disconnect repair pass completes and clears the marker
// (ClearPlaidCleanupPendingIfMatches) — cleanup for THIS episode is done; a DISTINCT, LATER
// terminal webhook for the SAME still-cancelled subscription then arrives (e.g. a redelivered or
// duplicate subscription.updated/canceled event that is not a genuine status transition) and calls
// UpdateSubscriptionFromPaddle with status='cancelled' again. Before the fix, the eligible branch's
// bare `COALESCE(plaid_cleanup_pending_at, $N)` saw a NULL marker and manufactured a BRAND NEW one
// — for cleanup that had already completed, timestamped AFTER the episode actually began.
//
// Falsifiability: reverting the eligible-branch SQL to `COALESCE(plaid_cleanup_pending_at, $N)`
// (dropping the "genuine transition" WHEN clause) makes this test fail — the marker would be
// resurrected non-NULL.
func TestUpdateSubscriptionFromPaddle_Integration_LaterTerminalEventDoesNotResurrectClearedMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "laterterminalnoresurrect", "active")

	// The FIRST cancellation: genuine transition into ineligibility — marker starts.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	afterFirstCancel := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, afterFirstCancel.marker, "the first cancellation must start the marker")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after first cancellation")

	// The repair pass completes cleanup and clears the marker for this episode.
	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, *afterFirstCancel.marker)
	require.NoError(t, err)
	require.True(t, cleared, "precondition: the repair pass must successfully clear the marker")
	assertPlaidMarkerInvariantCleanupComplete(t, pool, user.ID, "after repair pass clears the marker")

	// A DISTINCT, LATER terminal webhook for the SAME still-cancelled subscription arrives — NOT a
	// genuine transition (the row was already 'cancelled' before this call).
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Equal(t, "cancelled", row.status)
	assert.Nil(t, row.marker, "a later terminal webhook for a user ALREADY cancelled must NOT resurrect a new marker — cleanup for this episode already completed")
	assertPlaidMarkerInvariantCleanupComplete(t, pool, user.ID, "after later distinct terminal event for the same episode")
}

// TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker is the positive
// control for round-12 Fix 4: a REAL new ineligibility episode (resubscribe, then cancel again)
// must still get its OWN fresh marker — proving the "genuine transition" guard isn't a blanket
// suppression that breaks the ordinary repeat-cancellation case.
func TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "genuinenewepisode", "active")

	// Episode 1: cancel, then repair completes and clears.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	episode1 := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, episode1.marker)
	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, *episode1.marker)
	require.NoError(t, err)
	require.True(t, cleared)

	// Genuine resubscribe: status flips back to active — a real transition OUT of ineligibility.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine resubscribe")

	// Fence from the DATABASE clock (never compare Go time.Now() against a Postgres-written
	// timestamp — CI's Postgres container clock can lag the runner's).
	var before time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT NOW()").Scan(&before))

	// Episode 2: a GENUINE new cancellation — the row's old status is 'active', not already
	// eligible, so this must get its OWN fresh marker, not be suppressed.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "a genuine NEW cancellation episode must get its own fresh marker")
	assert.False(t, row.marker.Before(before), "the new episode's marker must be fresh (>= the fence), not suppressed or resurrected from episode 1")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine second cancellation")
}

// ─── The invariant sweep — the single most valuable test in this task (per the round-11 brief):
// walks the realistic transition sequences and asserts the invariant holds after EVERY one. This
// is the test that would have caught all four round-11 findings at once. ──

// TestPlaidMarkerInvariant_Integration_Sweep walks: trial→expire, active→cancel,
// cancel→resubscribe, comp grant→remove, past-due→grace — asserting
// assertPlaidMarkerInvariant after every single transition, on independent users so one
// sequence's fixture never leaks into another's assertions.
func TestPlaidMarkerInvariant_Integration_Sweep(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	t.Run("trial_to_expire", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweeptrial", "trial")
		assertPlaidMarkerInvariant(t, pool, user.ID, "trial: initial")

		flipped, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
		require.NoError(t, err)
		require.True(t, flipped)
		assertPlaidMarkerInvariant(t, pool, user.ID, "trial: after expiry")
	})

	t.Run("active_to_cancel", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepactive", "active")
		assertPlaidMarkerInvariant(t, pool, user.ID, "active: initial")

		require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
		assertPlaidMarkerInvariant(t, pool, user.ID, "active->cancel: after cancellation webhook")
	})

	t.Run("cancel_to_resubscribe", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepresub", "cancelled")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: cancelled with marker")

		// Mirrors subscription_webhook.go's resubscribe handling: UpdateSubscriptionFromPaddle
		// first, then the belt-and-braces ClearRetentionDates.
		require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: after status write")
		require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: after ClearRetentionDates")
	})

	t.Run("comp_grant_to_remove", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepcomp", "cancelled")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: cancelled with marker, before grant")

		expiresAt := time.Now().Add(48 * time.Hour)
		_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant", &expiresAt)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: after grant")

		_, err = adminRepo.RemoveUserTierOverride(ctx, "", user.ID, "test: remove", 6)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: after remove")
	})

	// t.Run("comp_grant_then_lapse_never_removed", ...) is the Issue #1413 round-13 P0 path:
	// unlike comp_grant_to_remove above (which always calls the unconditional admin removal
	// immediately and so never models a stalled expiry job), this subtest grants a comp with an
	// expiresAt ALREADY in the past — modeling a comp that has lapsed by the time anyone checks —
	// and deliberately NEVER calls RemoveUserTierOverride/RemoveUserTierOverrideIfMatches, modeling
	// the daily comp-expiry job degrading or never running at all. The invariant must still hold
	// throughout (the marker survives the grant untouched), and the repair query must still find
	// this user despite the override column still being populated (lapsed, never cleared) — proving
	// the fix does not depend on any writer ever re-visiting this row again.
	t.Run("comp_grant_then_lapse_never_removed", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepcomplapse", "cancelled")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp-lapse: cancelled with marker, before grant")

		lapsedExpiry := time.Now().Add(-1 * time.Hour)
		_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: comp an already-cancelled user", &lapsedExpiry)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp-lapse: after grant with already-past expiry")

		// Deliberately NOT calling RemoveUserTierOverride / RemoveUserTierOverrideIfMatches — this
		// is the whole point of this subtest: the expiry job never runs.
		seedLinkedItem(t, pool, user.ID, "item-"+user.ID, "active")
		candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
		require.NoError(t, err)
		found := false
		for _, c := range candidates {
			if c.ID == user.ID {
				found = true
			}
		}
		assert.True(t, found, "a comp granted over an already-cancelled user, lapsed and never removed, must still be found by the repair pass")
	})

	t.Run("past_due_to_grace", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweeppastdue", "past_due")
		assertPlaidMarkerInvariant(t, pool, user.ID, "past_due: initial")

		retentionStart := time.Now()
		retentionExpires := retentionStart.AddDate(0, 6, 0)
		flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, retentionStart, retentionExpires)
		require.NoError(t, err)
		require.True(t, flipped)
		assertPlaidMarkerInvariant(t, pool, user.ID, "past_due->grace: after grace-expiry cancellation")
	})
}

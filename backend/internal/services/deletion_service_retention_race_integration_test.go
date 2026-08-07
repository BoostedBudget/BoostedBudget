//go:build integration

package services

// deletion_service_retention_race_integration_test.go — real-Postgres proof that the RETENTION
// authorisation is re-verified against committed state at claim time, not merely at candidate-
// selection time (Issue #1440's T0 defect, in the sibling path T0 did not fix).
//
// The reachable sequence this file pins:
//
//  1. RetentionCleanupCheckWorker's sweep selects a PAGE of retention candidates
//     (GetUsersWithExpiredRetentionPage: expired retention clock, subscription_status in
//     ('inactive','cancelled'), no deletion request, no manual override).
//  2. The worker processes that page row by row, in process, so an arbitrary amount of time can
//     pass between selection and any individual row's ExecuteDeletion call.
//  3. Inside that window the user RESUBSCRIBES. The reactivation webhook path
//     (UpdateSubscriptionFromPaddle with an active status, then ClearRetentionDates —
//     subscription_webhook.go's processSubscriptionCreated/Activated) sets the status active and
//     NULLs retention_started_at/retention_expires_at plus both notice stamps.
//  4. The worker reaches that user. ExecuteDeletion re-reads the row but has no rule that rejects
//     an active paid subscription, so the only thing standing between a paying customer and a
//     hard delete is retentionDeletionClaimPredicate.
//
// Before this fix that predicate checked only "no deletion request, no manual override at all" —
// both still true of a freshly resubscribed user — so the claim passed, cleanupPaddleResources
// cancelled the user's brand-new subscription (it reads the row's raw status, which is now
// 'active'), and DeleteUserCascade hard-deleted them. The cascade's own in-transaction re-check
// could not catch it either: deletionAuthorisationUnionPredicate composes the same broad retention
// branch.
//
// Why integration and not pgxmock: the whole defect is which rows a SQL predicate matches against
// committed state. A mock matching query text and returning a canned RowsAffected passes over both
// the broken and the fixed predicate (see memory feedback_pgxmock_integration_coverage.md).
//
// The four tests are deliberately a set, not one headline plus decoration: the resubscription case
// alone would pass just as well if retention deletion had broken outright, so
// RetentionCandidate_IsDeleted pins that a genuine candidate is still hard-deleted, and the two
// half-tests pin each added conjunct (cleared clock; not-yet-elapsed clock) independently of the
// status half.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -tags integration -p 1 -count=1 \
//	  -run TestExecuteDeletion_Integration_Retention -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newRetentionRaceTestDeletionService is newEpisodeTestDeletionService plus a Paddle cleanup mock,
// because the harm this file is about is not only the hard delete: cleanupPaddleResources cancels
// the subscription of any user whose RAW subscription_status is active/trialing/past_due, and a
// resubscribed user's status is exactly that. Permissive expectations (not absent ones) so a
// wrongful call fails as a readable AssertNotCalled rather than a testify panic that aborts the
// whole binary — same reasoning as newEpisodeTestDeletionService's mocks.
func newRetentionRaceTestDeletionService(t *testing.T, f *integrationServiceFixture) (DeletionService, *MockClerkService, *MockAuditLogger, *MockPaddleDeletionCleaner) {
	t.Helper()
	ds, clerk, audit := newEpisodeTestDeletionService(t, f)
	paddle := new(MockPaddleDeletionCleaner)
	paddle.On("CancelSubscriptionImmediately", mock.Anything, mock.Anything).Return(nil).Maybe()
	paddle.On("ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	ds.SetPaddleCleanupService(paddle)
	return ds, clerk, audit, paddle
}

// retentionSweepPager is the one method RetentionCleanupCheckWorker uses to select candidates. It is
// not on the broad repository.ProfileRepository interface (the worker declares its own narrow
// dependency interface), so this test asserts its way to the concrete implementation rather than
// restating the candidate query — a hand-copied predicate here could agree with a wrong fix.
type retentionSweepPager interface {
	GetUsersWithExpiredRetentionPage(ctx context.Context, after *repository.RetentionSweepCursor, limit int) ([]*domain.User, error)
}

// requireIsRetentionSweepCandidate proves the fixture is genuinely selected by the production
// candidate query, walking every page exactly as RetentionCleanupCheckWorker does. Without this the
// tests below could assert "not deleted" about a user the sweep would never have reached, which is
// the failure mode where four tests and six mutations all pass over a broken fail-safe.
func requireIsRetentionSweepCandidate(t *testing.T, ctx context.Context, f *integrationServiceFixture, userID string) {
	t.Helper()
	pager, ok := f.profileRepo.(retentionSweepPager)
	require.True(t, ok, "the profile repository must expose the production retention candidate query")
	const pageSize = 500
	var cursor *repository.RetentionSweepCursor
	for {
		page, err := pager.GetUsersWithExpiredRetentionPage(ctx, cursor, pageSize)
		require.NoError(t, err)
		for _, u := range page {
			if u.ID == userID {
				return
			}
		}
		if len(page) < pageSize {
			t.Fatalf("fixture precondition: user %s must be selected by GetUsersWithExpiredRetentionPage, "+
				"otherwise this test says nothing about the retention deletion path", userID)
		}
		last := page[len(page)-1]
		cursor = &repository.RetentionSweepCursor{ExpiresAt: *last.RetentionExpiresAt, UserID: last.ID}
	}
}

// newRetentionRaceCandidate creates a user in the exact state the retention sweep selects and
// asserts that it is in fact selected.
func newRetentionRaceCandidate(t *testing.T, ctx context.Context, f *integrationServiceFixture, label string) string {
	t.Helper()
	email := fmt.Sprintf("%s-%d@example.com", label, time.Now().UnixNano())
	user := insertIntegrationTestUser(t, f.profileRepo, email, "premium", "cancelled")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID) //nolint:errcheck
	})
	armElapsedRetentionClock(t, ctx, f.pool, f.profileRepo, user.ID)
	requireIsRetentionSweepCandidate(t, ctx, f, user.ID)
	return user.ID
}

// resubscribe replays the production reactivation path: the subscription webhook writes an active
// status via UpdateSubscriptionFromPaddle and then calls ClearRetentionDates (both branches in
// subscription_webhook.go do exactly this pair, guarded by IsActiveSubscription()). Returns the
// Paddle subscription id it attached, which is what makes the wrongful-cancellation assertion
// possible.
func resubscribe(t *testing.T, ctx context.Context, f *integrationServiceFixture, userID string) string {
	t.Helper()
	subID := fmt.Sprintf("sub_x11_%d", time.Now().UnixNano())
	startedAt := time.Now()
	endsAt := startedAt.Add(30 * 24 * time.Hour)
	require.NoError(t, f.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, "premium", "active", &startedAt, &endsAt, &subID))
	require.NoError(t, f.profileRepo.ClearRetentionDates(ctx, userID))
	return subID
}

// TestExecuteDeletion_Integration_RetentionResubscribed_NotDeleted is the headline: a valid
// retention candidate who resubscribes inside the sweep's processing window must not be deleted,
// and must not have their new subscription cancelled on the way out.
func TestExecuteDeletion_Integration_RetentionResubscribed_NotDeleted(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	userID := newRetentionRaceCandidate(t, ctx, f, "retention-resubscribed")

	// The sweep has this user in its page. Now they resubscribe.
	subID := resubscribe(t, ctx, f, userID)

	// The worker reaches them.
	ds, clerk, audit, paddle := newRetentionRaceTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, userID, repository.DeletionKindRetention),
		"a candidate who resubscribed must be a benign no-op, not an error")

	// Asserted BEFORE the row check on purpose: cleanupPaddleResources runs between the claim and
	// the cascade, so cancelling a paying customer's brand-new subscription is a distinct, earlier
	// harm than the hard delete, and it must be visible in its own right rather than hidden behind
	// a fatal row-missing assertion.
	paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)

	requireDeletionSideEffectsNotRun(t, clerk, audit)
	requireUserStillPresent(t, f, userID,
		"a user who resubscribed after the retention sweep selected them must NOT be hard-deleted")

	after, err := f.profileRepo.GetUserByID(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "active", after.SubscriptionStatus,
		"the resubscription must survive untouched — status")
	require.NotNil(t, after.PaddleSubscriptionID, "the resubscription must survive untouched — subscription id")
	require.Equal(t, subID, *after.PaddleSubscriptionID)
}

// TestExecuteDeletion_Integration_RetentionCandidate_IsDeleted is the control that stops the test
// above from passing for the wrong reason. A candidate who does NOT resubscribe must still be hard
// deleted: the fix must narrow the claim to the conditions that authorised it, not disable
// retention deletion.
func TestExecuteDeletion_Integration_RetentionCandidate_IsDeleted(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	userID := newRetentionRaceCandidate(t, ctx, f, "retention-still-deleted")

	ds, clerk, audit, _ := newRetentionRaceTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, userID, repository.DeletionKindRetention))

	var exists bool
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists))
	require.False(t, exists,
		"an expired-retention candidate who did NOT resubscribe must still be hard-deleted — "+
			"DeleteUserCascade issues a real DELETE FROM users")

	clerk.AssertCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	audit.AssertCalled(t, "Log", mock.Anything, "user.deleted", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_Integration_RetentionResubscribedClockClearFailed_NotDeleted pins the
// SUBSCRIPTION-STATUS conjunct on its own, and exists because mutation testing showed nothing else
// did: in the headline test a resubscription clears the clock as well, so the clock conjuncts alone
// refuse that claim and the status conjunct could be deleted with every other test still green.
//
// The state is reachable, not contrived. Both reactivation branches in subscription_webhook.go call
// ClearRetentionDates on a BEST-EFFORT basis — its error is logged at Warn and swallowed, the
// webhook still succeeds — so a user can be genuinely active with an expired retention clock still
// on the row. Nothing re-arms the retention path against them except this predicate; the repair job
// (GetUsersNeedingRetentionClockRepair) only looks at rows whose clock is NULL.
func TestExecuteDeletion_Integration_RetentionResubscribedClockClearFailed_NotDeleted(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	userID := newRetentionRaceCandidate(t, ctx, f, "retention-clear-failed")

	// The reactivation lands, but its best-effort ClearRetentionDates does not (deliberately not
	// called here — that IS the failure being simulated).
	subID := fmt.Sprintf("sub_x11_stale_%d", time.Now().UnixNano())
	startedAt := time.Now()
	require.NoError(t, f.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, "premium", "active", &startedAt, nil, &subID))

	before, err := f.profileRepo.GetUserByID(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "active", before.SubscriptionStatus, "fixture precondition: the user is paying again")
	require.NotNil(t, before.RetentionExpiresAt,
		"fixture precondition: the stale expired clock must still be on the row, so this test isolates the status half")
	require.False(t, before.RetentionExpiresAt.After(time.Now()),
		"fixture precondition: the stale clock must still read as elapsed")

	ds, clerk, audit, paddle := newRetentionRaceTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, userID, repository.DeletionKindRetention))

	paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	requireDeletionSideEffectsNotRun(t, clerk, audit)
	requireUserStillPresent(t, f, userID,
		"an active, paying subscription must block a retention hard delete even when the stale retention clock survived")
}

// TestExecuteDeletion_Integration_RetentionClockCleared_StatusStillCancelled_NotDeleted pins the
// NULL-clock half of the fix on its own: the status stays 'cancelled', so only the
// retention_expires_at IS NOT NULL conjunct can refuse this claim.
//
// This state is reachable independently of a full resubscription — ClearRetentionDates is called on
// every active-status subscription webhook, and RepairRetentionClock's own guard
// (GetUsersNeedingRetentionClockRepair selects retention_expires_at IS NULL users) exists precisely
// because rows sit in it.
func TestExecuteDeletion_Integration_RetentionClockCleared_StatusStillCancelled_NotDeleted(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	userID := newRetentionRaceCandidate(t, ctx, f, "retention-clock-cleared")

	require.NoError(t, f.profileRepo.ClearRetentionDates(ctx, userID))

	before, err := f.profileRepo.GetUserByID(ctx, userID)
	require.NoError(t, err)
	require.Nil(t, before.RetentionExpiresAt, "fixture precondition: the clock must be cleared")
	require.Equal(t, "cancelled", before.SubscriptionStatus,
		"fixture precondition: the status must STILL be cancelled, so this test isolates the clock half")

	ds, clerk, audit, _ := newRetentionRaceTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, userID, repository.DeletionKindRetention))

	requireDeletionSideEffectsNotRun(t, clerk, audit)
	requireUserStillPresent(t, f, userID,
		"a user with no retention clock at all must not be hard-deleted under the retention authorisation")
}

// TestExecuteDeletion_Integration_RetentionExpiryInFuture_NotDeleted pins the not-yet-elapsed half
// on its own: the clock is non-NULL and the status is still 'cancelled', so only the
// retention_expires_at <= NOW() conjunct can refuse this claim.
//
// The clock is re-armed through the production writer (SetRetentionDates), which is what
// RepairRetentionClock's sibling path and a fresh cancellation both do — a new cancellation
// occurring inside the sweep window restarts the window, and the user is owed the whole of it.
func TestExecuteDeletion_Integration_RetentionExpiryInFuture_NotDeleted(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	userID := newRetentionRaceCandidate(t, ctx, f, "retention-expiry-future")

	// RetentionClockUnguarded: fixture setup, not a subscription transition (Issue #1500 residual).
	require.NoError(t, f.profileRepo.SetRetentionDates(ctx, userID, repository.RetentionClockUnguarded, retentionFixtureMonths))

	before, err := f.profileRepo.GetUserByID(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, before.RetentionExpiresAt, "fixture precondition: the clock must be armed")
	require.True(t, before.RetentionExpiresAt.After(time.Now()),
		"fixture precondition: the clock must NOT have elapsed")
	require.Equal(t, "cancelled", before.SubscriptionStatus,
		"fixture precondition: the status must STILL be cancelled, so this test isolates the expiry half")

	ds, clerk, audit, _ := newRetentionRaceTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, userID, repository.DeletionKindRetention))

	requireDeletionSideEffectsNotRun(t, clerk, audit)
	requireUserStillPresent(t, f, userID,
		"a user whose retention window has been restarted must not be hard-deleted before it elapses")
}

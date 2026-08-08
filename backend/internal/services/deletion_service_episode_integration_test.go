//go:build integration

package services

// deletion_service_episode_integration_test.go — real-Postgres proof that a STALE queued
// account-deletion job cannot hard-delete a user whose deletion-request EPISODE has since
// changed (Issue #1440, T0).
//
// Two sequences are covered, both of which were reachable and both of which were untested:
//
//  1. The user cancels. AccountDeletionWorker's job row is already queued (CancelDeletion does
//     not, and cannot reliably, cancel a job that may already be running), so it still runs and
//     still calls ExecuteDeletion. The user must NOT be deleted.
//
//  2. The user cancels and then re-requests deletion. The stale job from the FIRST episode is
//     still queued; it must not delete the user before the SECOND episode's own
//     deletion_scheduled_at has elapsed.
//
// Why this must be an integration test rather than a pgxmock unit test: the whole defect lives in
// the SQL predicate that authorises the claim, evaluated against committed row state. A mock that
// matches query text and returns a canned RowsAffected cannot distinguish the broken predicate
// from the fixed one (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -tags integration -p 1 -count=1 \
//	  -run TestExecuteDeletion_Integration_Episode -v

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newEpisodeTestDeletionService wires a DeletionService with everything ExecuteDeletion actually
// requires against a real pool: the real ProfileRepository and the real WorkClaimsRepository
// (ExecuteDeletion fails closed without the latter — see SetWorkClaimsRepo's interface doc).
//
// The Clerk and audit mocks are given PERMISSIVE expectations rather than none. Leaving them
// unexpected would also fail a wrongful deletion, but by panicking inside testify mid-test — which
// aborts the whole test binary and hides every later test in the file. Permissive expectations plus
// the explicit AssertNotCalled checks in each test below assert the same thing (deletion's
// post-cascade side effects must never run) while keeping the failure a readable assertion.
func newEpisodeTestDeletionService(t *testing.T, f *integrationServiceFixture) (DeletionService, *MockClerkService, *MockAuditLogger) {
	t.Helper()
	clerk := new(MockClerkService)
	clerk.On("DeleteUser", mock.Anything, mock.Anything).Return(nil).Maybe()
	audit := new(MockAuditLogger)
	audit.On("Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	ds := NewDeletionService(
		f.profileRepo,
		NewEmailServiceMock("", "", "https://app.example.com"),
		clerk,
		audit,
		nil,
		30,
		"test-secret-key",
	)
	ds.SetWorkClaimsRepo(repository.NewWorkClaimsRepository(f.pool))
	return ds, clerk, audit
}

// requireDeletionSideEffectsNotRun asserts the two irreversible side effects a completed deletion
// leaves behind — the Clerk account deletion, and the durable "user.deleted" audit entry — never
// happened. These are the observable evidence that the cascade committed, independent of whether
// the users row happens to still be readable.
//
// ⚠ THE AUDIT HALF IS CHECKED AGAINST THE DATABASE, NOT THE MOCK. Since Issue #1507 the entry is
// written inside DeleteUserCascade's own transaction (repository.LogInTx), so the mocked
// AuditLogger never sees it under ANY outcome — an AssertNotCalled on the mock alone would now be a
// test that cannot fail. Counting rows for this subject is the assertion that still discriminates.
func requireDeletionSideEffectsNotRun(t *testing.T, ctx context.Context, f *integrationServiceFixture, userID string, clerk *MockClerkService, audit *MockAuditLogger) {
	t.Helper()
	clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, "user.deleted", mock.Anything, mock.Anything)

	var auditRows int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action = 'user.deleted'`, userID).Scan(&auditRows))
	require.Zero(t, auditRows,
		"a deletion that did not happen must leave no durable `user.deleted` row claiming it did")
}

// requireUserStillPresent asserts the users row physically still exists and is not soft-deleted.
// DeleteUserCascade issues a real `DELETE FROM users` (Issue #1440), so a wrongful deletion shows
// up as a missing row, not as is_deleted = TRUE — both are checked so this assertion stays honest
// if that ever changes back.
func requireUserStillPresent(t *testing.T, f *integrationServiceFixture, userID, why string) {
	t.Helper()
	var isDeleted bool
	err := f.pool.QueryRow(context.Background(),
		`SELECT is_deleted FROM users WHERE id = $1`, userID).Scan(&isDeleted)
	require.NoError(t, err, why+" — the users row was hard-deleted")
	require.False(t, isDeleted, why+" — the users row was soft-deleted")
}

// TestExecuteDeletion_Integration_EpisodeCancelled_StaleJobDoesNotDelete is sequence 1.
//
// The user's grace period elapsed, so ScheduledDeletionCheckWorker enqueued an account_deletion
// job. Before that job ran, the user clicked the cancellation link: CancelDeletion NULLs
// deletion_requested_at and deletion_scheduled_at but leaves the queued job alone. The job then
// runs ExecuteDeletion against the user's CURRENT state.
//
// Pre-fix this deleted the user: ClaimForDeletion's predicate authorised the claim through its
// "OR (no manual tier override at all)" disjunct — which is true for any uncomped user,
// cancellation or not — and DeleteUserCascade's own re-check then compared deletion_requested_at
// against the NULL this same stale job had just read, so it matched too. Deleting data a user
// explicitly asked to keep inverts GDPR intent.
func TestExecuteDeletion_Integration_EpisodeCancelled_StaleJobDoesNotDelete(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	user := insertIntegrationTestUser(t, f.profileRepo, "episode-cancelled-"+t.Name()+"@example.com", "premium", "inactive")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID) //nolint:errcheck
	})

	// The user requested deletion and the grace period has already elapsed — exactly the state in
	// which GetUsersScheduledForDeletionPage selects them and a job gets enqueued.
	require.NoError(t, f.profileRepo.MarkForDeletion(ctx, user.ID, time.Now().Add(-1*time.Hour)))

	// ... and then the user cancels. No manual tier override is ever set: this user is uncomped,
	// which is what makes the pre-fix predicate's second disjunct true.
	require.NoError(t, f.profileRepo.CancelDeletion(ctx, user.ID))

	// The already-queued job now runs. This is the stale job.
	ds, clerk, audit := newEpisodeTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, user.ID, repository.DeletionKindUserRequested),
		"a stale job finding a cancelled request must be a benign no-op, not an error")
	requireDeletionSideEffectsNotRun(t, ctx, f, user.ID, clerk, audit)

	requireUserStillPresent(t, f, user.ID,
		"a user who cancelled their deletion request must not be deleted by an already-queued job")
}

// TestExecuteDeletion_Integration_EpisodeReRequested_StaleJobDoesNotDeleteEarly is sequence 2.
//
// Cancel-then-re-request. The stale job from the FIRST episode is still queued. It reads the
// SECOND episode's deletion_requested_at, which is non-NULL, so pre-fix it satisfied
// ClaimForDeletion's first disjunct outright — and nothing anywhere in ExecuteDeletion,
// ClaimForDeletion, or DeleteUserCascade compared deletion_scheduled_at to now. DeleteUserCascade's
// `deletion_requested_at IS NOT DISTINCT FROM $2` guard could not catch it either: the stale job
// read the NEW timestamp at its own start, so expected and actual agreed.
//
// The result was an immediate hard delete that skipped the entire new grace period. This window is
// real rather than theoretical: a cancellation token stays valid for a day past
// deletion_scheduled_at (set in RequestDeletion, checked in CancelDeletion — see this file's
// non-test sibling), so a user genuinely can cancel and re-request while an old job is still
// queued.
func TestExecuteDeletion_Integration_EpisodeReRequested_StaleJobDoesNotDeleteEarly(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	user := insertIntegrationTestUser(t, f.profileRepo, "episode-rerequest-"+t.Name()+"@example.com", "premium", "inactive")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID) //nolint:errcheck
	})

	// Episode 1: requested, grace elapsed, job enqueued.
	require.NoError(t, f.profileRepo.MarkForDeletion(ctx, user.ID, time.Now().Add(-1*time.Hour)))
	// Cancelled.
	require.NoError(t, f.profileRepo.CancelDeletion(ctx, user.ID))
	// Episode 2: re-requested, with a FRESH 30-day grace period that has NOT elapsed.
	newScheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, f.profileRepo.MarkForDeletion(ctx, user.ID, newScheduledAt))

	// Episode 1's job — still queued — now runs.
	ds, clerk, audit := newEpisodeTestDeletionService(t, f)
	require.NoError(t, ds.ExecuteDeletion(ctx, user.ID, repository.DeletionKindUserRequested),
		"a stale job whose episode has been superseded must be a benign no-op, not an error")
	requireDeletionSideEffectsNotRun(t, ctx, f, user.ID, clerk, audit)

	requireUserStillPresent(t, f, user.ID,
		"a re-requested deletion must not be executed before its own deletion_scheduled_at elapses")

	// The new episode must be intact — the stale job must not have consumed or altered it.
	after, err := f.profileRepo.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, after.DeletionScheduledAt)
	require.WithinDuration(t, newScheduledAt, *after.DeletionScheduledAt, time.Minute,
		"the second episode's schedule must survive the stale job untouched")
}

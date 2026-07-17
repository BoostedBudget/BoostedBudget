package services

// deletion_service_integration_test.go — end-to-end proof, against a REAL Postgres
// instance, of the owner-deletion guard from #1367 (Phase 3) interacting with
// HouseholdService.ForceDisband (#1371 — Task 0/7, Phase 7 "household offboarding").
//
// The pgxmock-based unit tests in deletion_service_test.go (see
// TestRequestDeletion_Transactional_OwnerWithActiveMembers_RollsBack and siblings)
// already cover the guard's branching in isolation. What they CANNOT prove is the
// full real-world sequence this test exists for: an owner blocked from deleting their
// account by active household members calls ForceDisband (a genuinely separate
// service, its own transaction) to dissolve the household, and THEN their account
// deletion request — re-run afterward, against the SAME real household row — succeeds.
// That is exactly the kind of multi-transaction, cross-service interaction pgxmock
// can't catch (see memory feedback_pgxmock_integration_coverage.md): a mocked
// CountActiveMembers call can't tell you whether ForceDisband's own DELETE FROM
// households commit actually made GetHouseholdForUser stop finding the household on
// the next real query.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestDeletionServiceIntegration -v
//
// Reuses the setupHouseholdServiceIntegration fixture (household_service_integration_test.go)
// for a real pool/profileRepo/HouseholdService, and the DeletionService test doubles
// (MockClerkService, MockAuditLogger) from deletion_service_test.go — same package.

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newIntegrationTestDeletionService wires a real DeletionService exactly as
// cmd/server/main.go does: HouseholdReader + HouseholdInviteRevoker + SetDB(pool).
// SetDB makes RequestDeletion take the transactional path
// (requestDeletionTransactional), which is what production actually runs — the
// HouseholdReader/HouseholdInviteRevoker wiring is present but superseded per the
// SetDB doc comment; wiring both here mirrors production. The returned mock audit
// logger expects exactly one "user.deletion_requested" log call for ownerID (the
// blocked-then-retried RequestDeletion calls in these tests only ever act on the
// owner, so a single expectation covers both attempts).
func newIntegrationTestDeletionService(t *testing.T, f *integrationServiceFixture, ownerID string) DeletionService {
	t.Helper()
	householdRepo := repository.NewHouseholdRepository(f.pool)
	inviteRepo := repository.NewHouseholdInviteRepository(f.pool, newTestFieldEncryptor(t))

	mockAudit := new(MockAuditLogger)
	mockAudit.On("Log", mock.Anything, "user.deletion_requested", "user", ownerID).Return(nil)
	ds := NewDeletionService(f.profileRepo, NewEmailServiceMock("", "", "https://app.example.com"), new(MockClerkService), mockAudit, nil, 30, "test-secret-key")
	ds.SetHouseholdReader(householdRepo)
	ds.SetHouseholdInviteRevoker(inviteRepo)
	ds.SetDB(f.pool)
	return ds
}

// TestDeletionServiceIntegration_ForceDisbandThenDelete_AllowsOwnerDeletion is the
// core new scenario for #1371 Task 8: owner + active member → owner's deletion request
// 409s → owner force-disbands (dissolving the household in one committed transaction,
// Task 0/7) → owner's deletion request, retried, now succeeds.
func TestDeletionServiceIntegration_ForceDisbandThenDelete_AllowsOwnerDeletion(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	member := insertIntegrationTestUser(t, f.profileRepo, "member-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, member.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, member.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	acceptResult, err := f.svc.AcceptInvite(ctx, member.ID, rawToken, true, nil)
	require.NoError(t, err)
	require.Equal(t, household.ID, acceptResult.Household.ID)

	// Household now has an active non-owner member — confirm the real repo agrees
	// before exercising the guard, so a failure below can't be blamed on fixture setup.
	householdRepo := repository.NewHouseholdRepository(f.pool)
	activeCount, err := householdRepo.CountActiveMembers(ctx, household.ID)
	require.NoError(t, err)
	require.Equal(t, 2, activeCount, "owner + member should both be active before the guard is exercised")

	ds := newIntegrationTestDeletionService(t, f, owner.ID)

	// 1. Owner's deletion request is blocked (409-mapped sentinel) while the member
	//    is still active.
	_, _, err = ds.RequestDeletion(ctx, owner.ID, owner.Email)
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)

	// Owner must NOT have been marked for deletion by the blocked attempt.
	ownerAfterBlock, err := f.profileRepo.GetUserByID(ctx, owner.ID)
	require.NoError(t, err)
	require.False(t, ownerAfterBlock.IsDeleted)
	require.Nil(t, ownerAfterBlock.DeletionScheduledAt)

	// 2. Owner force-disbands: dissolves the household, force-removes the member, all
	//    inside one committed transaction (Task 0/7 — already tested as its own unit
	//    in household_service_test.go; treated as a black box here).
	queued, err := f.svc.ForceDisband(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.Equal(t, member.Email, queued[0].ToEmail)

	// Household is really gone (real DB, not a mock's belief that it is).
	var householdCount int
	err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM households WHERE id = $1`, household.ID).Scan(&householdCount)
	require.NoError(t, err)
	require.Equal(t, 0, householdCount, "ForceDisband must have deleted the household row")

	// 3. Owner's deletion request, retried, now succeeds — the guard's
	//    GetHouseholdForUser no longer finds a household for this owner at all.
	cancellationToken, scheduledAt, err := ds.RequestDeletion(ctx, owner.ID, owner.Email)
	require.NoError(t, err, "deletion must succeed once the household has been force-disbanded")
	require.NotEmpty(t, cancellationToken)
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), scheduledAt, time.Minute)

	ownerAfterDeletion, err := f.profileRepo.GetUserByID(ctx, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, ownerAfterDeletion.DeletionScheduledAt, "owner must actually be marked for deletion in the real DB")
}

// TestDeletionServiceIntegration_OwnerAloneInHousehold_DeletionSucceedsWithoutForceDisband
// is a control case bracketing the scenario above: an owner with NO other active
// members (household of one) must be able to delete their account directly, without
// ever needing ForceDisband — proving the guard's ">1" threshold (not "household
// exists at all") against a real household row, not a mocked CountActiveMembers.
func TestDeletionServiceIntegration_OwnerAloneInHousehold_DeletionSucceedsWithoutForceDisband(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-solo-"+t.Name()+"@example.com", "household_2", "active")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner.ID) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Solo Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	ds := newIntegrationTestDeletionService(t, f, owner.ID)

	_, _, err = ds.RequestDeletion(ctx, owner.ID, owner.Email)
	require.NoError(t, err, "an owner with no other active members must be able to delete without force-disbanding first")

	ownerAfterDeletion, err := f.profileRepo.GetUserByID(ctx, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, ownerAfterDeletion.DeletionScheduledAt)

	// The household itself is left standing (RequestDeletion does not disband it) —
	// only ExecuteDeletion's cascade (out of scope for this test) or an explicit
	// disband would remove it.
	var householdCount int
	err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM households WHERE id = $1`, household.ID).Scan(&householdCount)
	require.NoError(t, err)
	require.Equal(t, 1, householdCount)
}

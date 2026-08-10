//go:build integration

package services

// deletion_service_effect_residue_integration_test.go — round 15, Tier A, proved end-to-end against
// a REAL Postgres.
//
// The unit siblings in deletion_service_effect_residue_test.go can only assert that
// RecordDeletionResidue was CALLED. That is not the property. The property is what an operator finds
// in the database when the dust settles, and it has two halves that a mock repository cannot express
// at all:
//
//   - a run that does not complete leaves a row — including the archive-without-cancel case (A1) and
//     the recognised-transient cascade error (A4), neither of which recorded anything before;
//   - a run that DOES complete leaves none, because DeleteUserCascade retracts the records inside the
//     same transaction that deletes the user. A mock cannot show you a row that was written and then
//     removed; it shows you the write and stops.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/ -tags integration -p 1 -count=1 \
//	  -run TestExecuteDeletion_Integration_Residue -v

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// residueCascadeStub delegates every ProfileRepository method to the REAL repository — including
// RecordDeletionResidue, which is the whole point: the residue writes under test are real INSERTs
// against real Postgres. Only DeleteUserCascade is replaced, because the outcomes these tests need
// (a refusal, a transient SQLSTATE) cannot be provoked from outside without corrupting the fixture,
// and provoking them is not what is being tested.
//
// A nil cascade means "use the real one", so the success case runs the genuine cascade — which is
// mandatory there, since the retraction being asserted lives inside it.
type residueCascadeStub struct {
	repository.ProfileRepository
	cascade func() (bool, error)
}

func (r *residueCascadeStub) DeleteUserCascade(ctx context.Context, userID string, expected *time.Time) (bool, error) {
	if r.cascade == nil {
		return r.ProfileRepository.DeleteUserCascade(ctx, userID, expected)
	}
	return r.cascade()
}

// residueFixture is a user who is eligible for a user-requested deletion right now and who has both
// a Paddle subscription and a Paddle customer, wired to a DeletionService with a mocked Paddle
// cleaner and the real work-claims repository.
type residueFixture struct {
	f      *integrationServiceFixture
	svc    DeletionService
	paddle *MockPaddleDeletionCleaner
	userID string
}

func newResidueFixture(t *testing.T, label, subscriptionStatus string, cascade func() (bool, error)) *residueFixture {
	t.Helper()
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	user := insertIntegrationTestUser(t, f.profileRepo, label+"-"+t.Name()+"@example.com", "premium", subscriptionStatus)
	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// so these DELETEs would silently no-op and leak rows into a different test on the shared DB.
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID) //nolint:errcheck
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)               //nolint:errcheck
	})

	_, err := f.pool.Exec(ctx,
		`UPDATE users SET paddle_subscription_id = $1, paddle_customer_id = $2 WHERE id = $3`,
		"sub_"+user.ID, "ctm_"+user.ID, user.ID)
	require.NoError(t, err)

	// Requested, and the grace period has already elapsed — the state in which a deletion job runs.
	require.NoError(t, f.profileRepo.MarkForDeletion(ctx, user.ID, time.Now().Add(-1*time.Hour)))

	paddle := new(MockPaddleDeletionCleaner)
	paddle.On("CancelSubscriptionImmediately", mock.Anything, mock.Anything).Return(nil).Maybe()
	paddle.On("ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	clerk := new(MockClerkService)
	clerk.On("DeleteUser", mock.Anything, mock.Anything).Return(nil).Maybe()
	audit := new(MockAuditLogger)
	audit.On("Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	svc := NewDeletionService(
		&residueCascadeStub{ProfileRepository: f.profileRepo, cascade: cascade},
		NewEmailServiceMock("", "", "https://app.example.com"),
		clerk, audit, nil, 30, "test-secret-key",
	)
	svc.SetPaddleCleanupService(paddle)
	svc.SetWorkClaimsRepo(repository.NewWorkClaimsRepository(f.pool))

	return &residueFixture{f: f, svc: svc, paddle: paddle, userID: user.ID}
}

// residueActions returns the residue action values on record for this subject, so every assertion
// below is MEMBERSHIP of a named effect rather than a count that a broadened predicate would satisfy
// just as well. The pattern is written out here rather than imported from the production constant:
// this is the independent enumeration that constant is being checked against.
func (r *residueFixture) residueActions(t *testing.T) []string {
	t.Helper()
	rows, err := r.f.pool.Query(context.Background(),
		`SELECT action FROM audit_logs WHERE resource_id = $1 AND action LIKE 'user.deletion_residue.%' ORDER BY action`,
		r.userID)
	require.NoError(t, err)
	defer rows.Close()

	var actions []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		actions = append(actions, a)
	}
	require.NoError(t, rows.Err())
	return actions
}

// FINDING A1, END TO END, ON THE PATH THAT MAKES IT DETERMINISTIC. An ex-subscriber — raw status
// "cancelled", so there is nothing to cancel — still has their Paddle CUSTOMER archived and
// anonymised, which cleanupPaddleResources itself calls "THE ONE ACTION NOTHING CAN UNDO". The
// cascade then declines (the user cancelled their deletion, or the C2 guard refuses). Before round 15
// the residue mechanism keyed entirely off the subscription cancel, so this run produced an Info log
// and nothing else, forever.
func TestExecuteDeletion_Integration_ResidueArchiveWithoutCancel_IsRecorded(t *testing.T) {
	r := newResidueFixture(t, "residue-archive", "cancelled", func() (bool, error) { return false, nil })

	require.NoError(t, r.svc.ExecuteDeletion(context.Background(), r.userID, repository.DeletionKindUserRequested),
		"a cascade that refuses is a normal outcome, not an error")

	r.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	r.paddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	assert.Equal(t, []string{"user.deletion_residue.paddle_customer_archived"}, r.residueActions(t),
		"the unrecoverable act must be on record, and under its own effect — not silently absent and not mislabelled as a subscription cancellation")
}

// FINDING A4, END TO END — AND THIS IS THE TEST THAT FAILS UNDER THE OPPOSITE DECISION. A recognised
// transient cascade error (40P01, deadlock_detected) used to record nothing at all, on round 14's
// argument that the retry reconciles. Round 15 records the landing effects anyway, because the retry
// now genuinely reconciles them (a completed cascade retracts the rows — see the success sibling
// below) whereas before it merely was assumed to.
//
// A version that kept the silence produces an EMPTY list here. A version that dropped round 14's
// classifier altogether adds the ..._cascade_failed action, which is asserted absent: the classifier
// is kept, doing the one job it was designed for.
func TestExecuteDeletion_Integration_ResidueTransientCascadeError_IsRecorded(t *testing.T) {
	r := newResidueFixture(t, "residue-transient", "active", func() (bool, error) { return false, wrapPgError("40P01") })

	require.Error(t, r.svc.ExecuteDeletion(context.Background(), r.userID, repository.DeletionKindUserRequested))

	assert.Equal(t, []string{
		"user.deletion_residue.paddle_customer_archived",
		"user.deletion_residue.paddle_subscription_cancelled",
	}, r.residueActions(t),
		"both irreversible calls went out and the user is still here — a retry that never comes is the case this record exists for")
}

// The other half of round 14's classifier, still doing its job: a NON-transient cascade error adds
// the second, different work item — "and this is stuck, fix the database fault too".
func TestExecuteDeletion_Integration_ResiduePersistentCascadeError_AlsoRecordsTheStuckKind(t *testing.T) {
	r := newResidueFixture(t, "residue-persistent", "active", func() (bool, error) { return false, wrapPgError("42501") })

	require.Error(t, r.svc.ExecuteDeletion(context.Background(), r.userID, repository.DeletionKindUserRequested))

	assert.Equal(t, []string{
		"user.deletion_residue.paddle_customer_archived",
		"user.deletion_residue.paddle_subscription_cancelled",
		"user.deletion_residue.paddle_subscription_cancelled_cascade_failed",
	}, r.residueActions(t))
}

// ⚠ SIBLING, MANDATORY, AND THE ONE THAT MAKES WRITE-AHEAD RECORDING DEFENSIBLE AT ALL. An ordinary
// successful deletion issues both irreversible calls and therefore writes both records — and must
// leave NONE of them behind, because DeleteUserCascade retracts them inside the same transaction
// that deletes the user. Without this, the mechanism would raise an alarm on every ordinary deletion
// of a paying customer and be switched off within a week.
//
// This one runs the REAL cascade (cascade == nil); stubbing it would remove the very statement under
// test.
func TestExecuteDeletion_Integration_ResidueOrdinarySuccess_LeavesNoRow(t *testing.T) {
	r := newResidueFixture(t, "residue-success", "active", nil)
	ctx := context.Background()

	require.NoError(t, r.svc.ExecuteDeletion(ctx, r.userID, repository.DeletionKindUserRequested))

	// The deletion really completed — otherwise "no residue rows" would be trivially true for the
	// wrong reason, which is exactly the shape of failure this batch was warned about.
	var userRows int
	require.NoError(t, r.f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, r.userID).Scan(&userRows))
	require.Zero(t, userRows, "sanity: this run must actually have deleted the user")
	var deletedAudit int
	require.NoError(t, r.f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action = 'user.deleted'`, r.userID).Scan(&deletedAudit))
	require.Equal(t, 1, deletedAudit, "sanity: the cascade committed its own terminal audit entry")

	r.paddle.AssertCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	r.paddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	assert.Empty(t, r.residueActions(t),
		"a deletion that completed leaves no work item — the records it wrote on the way through are retracted with the user")
}

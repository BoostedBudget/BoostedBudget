//go:build integration

package jobs

// retention_cleanup_check_occurrence_integration_test.go — real-Postgres proof that Issue #1413
// P5's composite occurrence key (userID + retention_expires_at) actually gates
// processRetentionNotices's email send at the CALLER boundary, across a genuine re-arm cycle.
//
// This is the specific regression the #1413 P5 task exists to fix for retention notices: before
// the fix, ActionRetentionNotice30d/7d were claimed on the bare userID, so once a user's FIRST
// retention cycle sent its 30-day notice and completed the claim, a user who later resubscribed
// (clearing the clock) and cancelled again — re-arming the clock with a NEW retention_expires_at —
// would find the SAME (resource_type, resource_id, action) row already completed and never be
// warned again before the SECOND cycle's hard deletion. pgxmock cannot prove this: it only echoes
// canned rows and cannot show two DIFFERENT composite keys behaving differently against the same
// UNIQUE constraint.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... -run TestRetentionCleanupCheck_Occurrence -tags integration -v

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestRetentionCleanupCheck_Occurrence_ReArmedCycle_30dNoticeFiresAgain is the regression test:
// a completed 30-day notice from an EARLIER retention cycle must not block the notice for a
// genuinely NEW cycle (a different retention_expires_at) for the same user.
func TestRetentionCleanupCheck_Occurrence_ReArmedCycle_30dNoticeFiresAgain(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := "user-" + uuid.New().String()
	firstCycleExpires := time.Now().Add(25 * 24 * time.Hour)
	// Re-armed: the user resubscribed (ClearRetentionDates) and later cancelled again, restarting
	// the 6-month clock from a later point in time — a genuinely NEW retention cycle.
	secondCycleExpires := time.Now().Add(28 * 24 * time.Hour)

	firstResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&firstCycleExpires))
	require.NoError(t, err)
	secondResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&secondCycleExpires))
	require.NoError(t, err)
	require.NotEqual(t, firstResourceID, secondResourceID,
		"test setup sanity: the two retention cycles must be genuinely different occurrences")

	t.Cleanup(func() {
		for _, rid := range []string{firstResourceID, secondResourceID} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
				repository.ResourceTypeUser, rid, repository.ActionRetentionNotice30d)
		}
	})

	// The FIRST retention cycle's 30-day notice already sent and completed (e.g. yesterday's run).
	firstToken, claimed, err := claimsRepo.Claim(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionRetentionNotice30d, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	ok, err := claimsRepo.Complete(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionRetentionNotice30d, firstToken)
	require.NoError(t, err)
	require.True(t, ok)

	user := &domain.User{ID: userID, Email: "rearm@example.com", RetentionExpiresAt: &secondCycleExpires}

	mockRepo := new(MockRetentionProfileRepository)
	mockRepo.On("GetUsersWithMalformedTierOverride", mock.Anything).Return([]*domain.User{}, nil).Maybe()
	mockRepo.On("GetOrphanedFKlessUserScopedRowCounts", mock.Anything).Return(repository.OrphanedFKlessUserScopedRowCounts{}, nil).Maybe()
	mockRepo.On("CountRetentionDeletionsWithheld", mock.Anything).Return(repository.RetentionWithholdingCounts{}, nil).Maybe()
	mockRepo.On("StampMissingPlaidCleanupMarkers", mock.Anything).Return(0, nil)
	mockRepo.On("ClearCompletedPlaidCleanupMarkers", mock.Anything).Return(0, nil)
	mockRepo.On("ClearMarkersOnEligibleStatus", mock.Anything).Return(0, nil).Maybe()
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{user}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetentionPage", ctx, mock.Anything, mock.Anything).Return([]*domain.User{}, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, userID, secondCycleExpires).Return(true, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, userID).Return(nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, user.Email, mock.Anything, mock.Anything).Return(nil).Once()

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetWorkClaimsRepo(claimsRepo)
	// Issue #1413 round-10: repairPlaidDisconnects now treats a nil plaidCleanup as a hard
	// configuration error — this test doesn't care about Plaid-repair mechanics (the candidate
	// list above is stubbed empty), but needs a wired, otherwise-inert collaborator to avoid
	// failing the whole job on that account alone.
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))

	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)

	require.NoError(t, err,
		"THE REGRESSION THIS TASK FIXES: a re-armed retention cycle must still be able to send its "+
			"30-day notice, even though an EARLIER cycle for the same user already completed this "+
			"action — this is exactly the re-arm logic added earlier in this branch, which the bare-"+
			"userID claim key silently defeated")
	mockEmail.AssertCalled(t, "SendRetentionNoticeEmail", ctx, user.Email, mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_Occurrence_SameCycle_SecondRunDoesNotResend proves the other
// direction: a retry within the SAME retention cycle (same retention_expires_at) must NOT resend
// the notice — the composite key must collide with itself across runs of the SAME occurrence.
func TestRetentionCleanupCheck_Occurrence_SameCycle_SecondRunDoesNotResend(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := "user-" + uuid.New().String()
	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)

	resourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&retentionExpiresAt))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d)
	})

	user := &domain.User{ID: userID, Email: "samecycle@example.com", RetentionExpiresAt: &retentionExpiresAt}

	mockRepo := new(MockRetentionProfileRepository)
	mockRepo.On("GetUsersWithMalformedTierOverride", mock.Anything).Return([]*domain.User{}, nil).Maybe()
	mockRepo.On("GetOrphanedFKlessUserScopedRowCounts", mock.Anything).Return(repository.OrphanedFKlessUserScopedRowCounts{}, nil).Maybe()
	mockRepo.On("CountRetentionDeletionsWithheld", mock.Anything).Return(repository.RetentionWithholdingCounts{}, nil).Maybe()
	mockRepo.On("StampMissingPlaidCleanupMarkers", mock.Anything).Return(0, nil)
	mockRepo.On("ClearCompletedPlaidCleanupMarkers", mock.Anything).Return(0, nil)
	mockRepo.On("ClearMarkersOnEligibleStatus", mock.Anything).Return(0, nil).Maybe()
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{user}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetentionPage", ctx, mock.Anything, mock.Anything).Return([]*domain.User{}, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, userID, retentionExpiresAt).Return(true, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, userID).Return(nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, user.Email, mock.Anything, mock.Anything).Return(nil).Once()

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetWorkClaimsRepo(claimsRepo)
	// Issue #1413 round-10: see the matching comment in
	// TestRetentionCleanupCheck_Occurrence_ReArmedCycle_30dNoticeFiresAgain above.
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))

	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err = worker.Work(ctx, job)
	require.NoError(t, err, "first run: the notice sends successfully")

	// Same run again (e.g. tomorrow's daily job, before the retention clock changes) — same
	// retention_expires_at, same occurrence. mockEmail's stub is registered with .Once(), so a
	// second real send attempt would panic on an unexpected call rather than merely fail an
	// assertion.
	err = worker.Work(ctx, job)
	require.NoError(t, err, "an already-sent notice for the SAME cycle is a legitimate skip, not a failure")

	mockEmail.AssertNumberOfCalls(t, "SendRetentionNoticeEmail", 1)
}

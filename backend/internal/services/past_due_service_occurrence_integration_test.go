package services

// past_due_service_occurrence_integration_test.go — real-Postgres proof that Issue #1413 P5's
// composite occurrence key (userID + past_due_started_at) actually gates
// processPlaidDisconnectWindow / processGraceExpiry's external Plaid/Paddle calls at the CALLER
// boundary, across a genuine recover -> re-enter-past-due lifecycle. Mirrors
// plaid_item_cleanup_service_side_effect_claim_integration_test.go's convention: no build tag,
// skips at runtime if DATABASE_URL is unset — pgxmock can only match query text and cannot prove
// that two DIFFERENT composite keys behave differently against the same UNIQUE constraint (see
// internal/repository/work_claims_occurrence_integration_test.go for the generic mechanism proof;
// this proves PastDueService actually wires that mechanism into a real "the collaborator ran/did
// not run" outcome).
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/services/ -run TestPastDueOccurrence -v

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func connectPastDueOccurrenceTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestPastDueOccurrence_PlaidDisconnect_SameCycle_SecondCallSkipsDisconnect proves a retry within
// the SAME past_due cycle (same past_due_started_at) must NOT disconnect Plaid twice — the
// composite key must collide with itself.
func TestPastDueOccurrence_PlaidDisconnect_SameCycle_SecondCallSkipsDisconnect(t *testing.T) {
	pool := connectPastDueOccurrenceTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := uuid.New().String()
	pastDueStarted := time.Now().Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	resourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&pastDueStarted))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue)
	})

	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	svc.SetWorkClaimsRepo(claimsRepo)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil).Once()
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err = svc.ProcessPastDueUser(ctx, user, time.Now())
	require.NoError(t, err, "first run: the disconnect succeeds")

	// A retry of the SAME past_due cycle (e.g. the daily cron runs again the next day, still
	// inside the Plaid-disconnect window) — Plaid must NOT be disconnected again. plaid's stub is
	// registered with .Once(), so a second real call would panic on an unexpected call rather
	// than merely fail an assertion.
	err = svc.ProcessPastDueUser(ctx, user, time.Now())
	require.NoError(t, err, "an already-completed claim is a legitimate skip, not a failure")

	plaid.AssertNumberOfCalls(t, "DisconnectAllUserItems", 1)
}

// TestPastDueOccurrence_PlaidDisconnect_NewCycle_AfterCompleted_DisconnectsAgain is the regression
// test: a completed Plaid disconnect from an EARLIER past_due cycle must not block disconnecting
// again in a genuinely NEW cycle (a different past_due_started_at) for the same user.
func TestPastDueOccurrence_PlaidDisconnect_NewCycle_AfterCompleted_DisconnectsAgain(t *testing.T) {
	pool := connectPastDueOccurrenceTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := uuid.New().String()
	firstCycleStarted := time.Now().Add(-40 * 24 * time.Hour)
	secondCycleStarted := time.Now().Add(-15 * 24 * time.Hour)

	firstResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&firstCycleStarted))
	require.NoError(t, err)
	secondResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&secondCycleStarted))
	require.NoError(t, err)
	require.NotEqual(t, firstResourceID, secondResourceID,
		"test setup sanity: the two past_due cycles must be genuinely different occurrences")

	t.Cleanup(func() {
		for _, rid := range []string{firstResourceID, secondResourceID} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
				repository.ResourceTypeUser, rid, repository.ActionPlaidDisconnectPastDue)
		}
	})

	// The FIRST past_due cycle's Plaid disconnect already completed (e.g. yesterday's dunning
	// run).
	firstToken, claimed, err := claimsRepo.Claim(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	ok, err := claimsRepo.Complete(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionPlaidDisconnectPastDue, firstToken)
	require.NoError(t, err)
	require.True(t, ok)

	// The user recovered, then entered a SECOND, genuinely new past_due cycle.
	user := userWithPastDue(userID, secondCycleStarted)

	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	svc.SetWorkClaimsRepo(claimsRepo)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil).Once()
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err = svc.ProcessPastDueUser(ctx, user, time.Now())

	require.NoError(t, err,
		"THE REGRESSION THIS TASK FIXES: a NEW past_due cycle must still be able to claim and "+
			"disconnect Plaid, even though an EARLIER cycle for the same user already completed "+
			"this action")
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// TestPastDueOccurrence_GraceCancel_NewCycle_AfterCompleted_CancelsAgain mirrors the Plaid-
// disconnect regression test for the OTHER recurring past_due action: a completed grace-cancel
// (auto-cancellation) from an EARLIER dunning cycle must not block auto-cancelling again in a
// genuinely NEW cycle (resubscribe, then go past_due and hit grace expiry a second time).
func TestPastDueOccurrence_GraceCancel_NewCycle_AfterCompleted_CancelsAgain(t *testing.T) {
	pool := connectPastDueOccurrenceTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := uuid.New().String()
	firstCycleStarted := time.Now().Add(-100 * 24 * time.Hour)
	secondCycleStarted := time.Now().Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)

	firstResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&firstCycleStarted))
	require.NoError(t, err)
	secondResourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&secondCycleStarted))
	require.NoError(t, err)
	require.NotEqual(t, firstResourceID, secondResourceID,
		"test setup sanity: the two dunning cycles must be genuinely different occurrences")

	t.Cleanup(func() {
		for _, rid := range []string{firstResourceID, secondResourceID} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action IN ($3, $4)`,
				repository.ResourceTypeUser, rid, repository.ActionPastDueGraceCancel, repository.ActionPaddleCancelSubscription)
		}
	})

	// The FIRST dunning cycle's grace-cancel already completed (e.g. an earlier auto-cancellation).
	firstToken, claimed, err := claimsRepo.Claim(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	ok, err := claimsRepo.Complete(ctx, repository.ResourceTypeUser, firstResourceID, repository.ActionPastDueGraceCancel, firstToken)
	require.NoError(t, err)
	require.True(t, ok)

	// The user later resubscribed and re-entered past_due, now past grace again — a SECOND,
	// genuinely new dunning cycle.
	user := userWithPastDue(userID, secondCycleStarted)

	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	svc.SetWorkClaimsRepo(claimsRepo)

	subID := *user.PaddleSubscriptionID
	now := time.Now()
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err = svc.ProcessPastDueUser(ctx, user, now)

	require.NoError(t, err,
		"THE REGRESSION THIS TASK FIXES: a NEW dunning cycle must still be able to claim and "+
			"auto-cancel, even though an EARLIER cycle for the same user already completed this "+
			"action")
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
}

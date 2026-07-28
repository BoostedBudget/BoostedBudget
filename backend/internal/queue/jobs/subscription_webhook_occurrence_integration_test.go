//go:build integration

package jobs

// subscription_webhook_occurrence_integration_test.go — real-Postgres proof that Issue #1413 P5's
// composite occurrence key (userID + Paddle event ID) actually gates
// processSubscriptionCanceled's Plaid disconnect / retention-clock writes at the CALLER boundary,
// end to end, across a genuine cancel -> resubscribe -> cancel again lifecycle.
//
// This is the specific regression the #1413 P5 task exists to fix: before the fix,
// ActionSubscriptionCancelledCleanup was claimed on the bare userID, so once the FIRST
// cancellation completed the claim, the SECOND cancellation (after a resubscribe) would find the
// same (resource_type, resource_id, action) row already completed and skip Plaid disconnect and
// the retention-clock write entirely — silently, because ClaimAlreadyCompleted correctly maps to
// success. pgxmock cannot prove this: it only echoes canned rows and cannot show two DIFFERENT
// composite keys behaving differently against the same UNIQUE constraint.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... -run TestSubscriptionWebhook_Occurrence -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestSubscriptionWebhook_Occurrence_CancelResubscribeCancelAgain_BothDisconnectAndStartRetention
// is the end-to-end regression test: two cancellation webhooks for the SAME user, carrying
// DIFFERENT Paddle event IDs (as a genuine resubscribe-then-cancel-again sequence would), must
// BOTH disconnect Plaid and BOTH (re)start the retention clock — not just the first.
func TestSubscriptionWebhook_Occurrence_CancelResubscribeCancelAgain_BothDisconnectAndStartRetention(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsOutcomeRepository(pool)

	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	worker.SetWorkClaimsRepo(claimsRepo)

	// The webhook fixture's payload bakes in custom_data.user_id -- processSubscriptionCanceled
	// extracts userID from THAT JSON field, not from any test parameter, so this must match the
	// fixture rather than a freshly generated ID.
	const userID = "user_2abc123def456"
	clerkUserID := "clerk_user_123"
	canceledUser := &domain.User{ID: userID, ClerkUserID: clerkUserID, SubscriptionTier: "premium"}

	firstWebhook := loadWebhookFixture(t, "subscription_canceled.json")
	firstWebhook.ID = "webhook-first-cancellation"
	// Randomize the event ID so repeated test runs (and the sibling test below, which shares the
	// same fixture and userID) never collide on a leftover work_claims row from a prior/crashed
	// run — mirrors the repository package's setupTestResource convention of a fresh uuid per test.
	firstWebhook.EventID = firstWebhook.EventID + "-" + uuid.New().String()

	// The second cancellation is a DIFFERENT Paddle event (Paddle issues a fresh event_id per
	// physical event) — this is what "resubscribe, then cancel again" looks like at the webhook
	// layer, even for the same underlying subscription.
	secondWebhook := loadWebhookFixture(t, "subscription_canceled.json")
	secondWebhook.ID = "webhook-second-cancellation"
	secondWebhook.EventID = firstWebhook.EventID + "-resubscribe-then-cancel-again"

	firstResourceID, err := repository.OccurrenceResourceID(userID, firstWebhook.EventID)
	require.NoError(t, err)
	secondResourceID, err := repository.OccurrenceResourceID(userID, secondWebhook.EventID)
	require.NoError(t, err)
	require.NotEqual(t, firstResourceID, secondResourceID,
		"test setup sanity: the two cancellations must be genuinely different occurrences")

	t.Cleanup(func() {
		for _, rid := range []string{firstResourceID, secondResourceID} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
				repository.ResourceTypeUser, rid, repository.ActionSubscriptionCancelledCleanup)
		}
	})

	mockWebhookRepo.On("GetByID", ctx, "webhook-first-cancellation").Return(firstWebhook, nil)
	mockWebhookRepo.On("GetByID", ctx, "webhook-second-cancellation").Return(secondWebhook, nil)
	// GetUserByID is called multiple times per webhook (initial read, post-write re-read,
	// post-claim re-verify) — the user's state never changes across any of those reads in this
	// test, so one open-ended stub correctly satisfies all of them across both deliveries.
	mockProfileRepo.On("GetUserByID", ctx, userID).Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, userID, "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, clerkUserID, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, userID).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, userID).Return(nil).Twice()
	mockProfileRepo.On("SetRetentionDates", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil).Twice()
	mockWebhookRepo.On("MarkProcessed", ctx, "webhook-first-cancellation").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "webhook-second-cancellation").Return(nil)

	job1 := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "webhook-first-cancellation",
		EventID:   firstWebhook.EventID,
		EventType: "subscription.canceled",
	})
	err = worker.Work(ctx, job1)
	require.NoError(t, err, "the first cancellation must process cleanly")

	job2 := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "webhook-second-cancellation",
		EventID:   secondWebhook.EventID,
		EventType: "subscription.canceled",
	})
	err = worker.Work(ctx, job2)
	require.NoError(t, err,
		"THE REGRESSION THIS TASK FIXES: cancel -> resubscribe -> cancel again must still process "+
			"the SECOND cancellation as a genuinely new occurrence, not be silently skipped as "+
			"already-completed from the first")

	mockPlaid.AssertNumberOfCalls(t, "DisconnectAllUserItems", 2)
	mockProfileRepo.AssertNumberOfCalls(t, "SetRetentionDates", 2)
}

// TestSubscriptionWebhook_Occurrence_SameEventRedelivered_SecondDeliveryDoesNotDisconnectAgain is
// the flip side: a REDELIVERY of the SAME Paddle event (same event_id — e.g. Paddle retries the
// webhook, or River retries the job after a transient failure downstream of MarkProcessed) must
// collide on the SAME composite key and be treated as already done — Plaid must NOT be
// disconnected a second time for what is, from Paddle's perspective, one event.
func TestSubscriptionWebhook_Occurrence_SameEventRedelivered_SecondDeliveryDoesNotDisconnectAgain(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsOutcomeRepository(pool)

	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	worker.SetWorkClaimsRepo(claimsRepo)

	// The webhook fixture's payload bakes in custom_data.user_id -- processSubscriptionCanceled
	// extracts userID from THAT JSON field, not from any test parameter.
	const userID = "user_2abc123def456"
	clerkUserID := "clerk_user_123"
	canceledUser := &domain.User{ID: userID, ClerkUserID: clerkUserID, SubscriptionTier: "premium"}

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	webhook.ID = "webhook-redelivered"
	// Randomize the event ID so repeated test runs never collide on a leftover work_claims row.
	webhook.EventID = webhook.EventID + "-" + uuid.New().String()

	resourceID, err := repository.OccurrenceResourceID(userID, webhook.EventID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup)
	})

	mockWebhookRepo.On("GetByID", ctx, "webhook-redelivered").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, userID).Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, userID, "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, clerkUserID, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, userID).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, userID).Return(nil).Once()
	mockProfileRepo.On("SetRetentionDates", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockWebhookRepo.On("MarkProcessed", ctx, "webhook-redelivered").Return(nil)

	job := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "webhook-redelivered",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	})

	err = worker.Work(ctx, job)
	require.NoError(t, err, "first delivery must process cleanly")

	// A second Work() call against the SAME webhook row (simulating a River retry, or the
	// webhook being reprocessed before MarkProcessed's effect is observed) must not disconnect
	// Plaid or write the retention clock again — the mocks above are stubbed with .Once(), so a
	// second real invocation would panic on an unexpected call rather than merely fail an
	// assertion.
	err = worker.Work(ctx, job)
	require.NoError(t, err, "a redelivery of the SAME event must be a legitimate no-op, not a failure")

	mockPlaid.AssertNumberOfCalls(t, "DisconnectAllUserItems", 1)
	mockProfileRepo.AssertNumberOfCalls(t, "SetRetentionDates", 1)
}

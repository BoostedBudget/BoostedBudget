package jobs

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRetiredSubscriptionWorkersDrainWithoutDependencies(t *testing.T) {
	require.NoError(t, (&RetiredSubscriptionWorker[TrialExpirationCheckArgs]{}).Work(context.Background(), &river.Job[TrialExpirationCheckArgs]{}))
	require.NoError(t, (&RetiredSubscriptionWorker[PastDueCheckArgs]{}).Work(context.Background(), &river.Job[PastDueCheckArgs]{}))
	require.NoError(t, (&RetiredSubscriptionWorker[PaddleReconciliationArgs]{}).Work(context.Background(), &river.Job[PaddleReconciliationArgs]{}))
	require.NoError(t, (&RetiredSubscriptionWorker[ManualTierOverrideExpiryCheckArgs]{}).Work(context.Background(), &river.Job[ManualTierOverrideExpiryCheckArgs]{}))
}

func TestSubscriptionWebhookRevenueCatAuthorityRetainsReceiptWithoutAccessWrites(t *testing.T) {
	for _, eventType := range []string{"subscription.created", "subscription.activated", "subscription.updated", "subscription.canceled", "subscription.paused", "subscription.past_due", "subscription.trialing", "subscription.resumed"} {
		t.Run(eventType, func(t *testing.T) {
			repo := &mockPaddleWebhookRepository{}
			repo.On("GetByID", mock.Anything, "receipt").Return(&domain.PaddleWebhookEvent{ID: "receipt", EventType: eventType, SignatureValid: true}, nil).Once()
			repo.On("MarkProcessed", mock.Anything, "receipt").Return(nil).Once()
			worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{PaddleWebhookRepo: repo})
			worker.SetRevenueCatAuthority()
			require.NoError(t, worker.Work(context.Background(), &river.Job[SubscriptionWebhookArgs]{JobRow: &rivertype.JobRow{Kind: "subscription_webhook", Queue: "high"}, Args: SubscriptionWebhookArgs{WebhookID: "receipt"}}))
			repo.AssertExpectations(t)
		})
	}
}

func TestSubscriptionWebhookRevenueCatAuthorityStillAuditsPayments(t *testing.T) {
	worker, receipts, _, _, _, transactions := setupWorkerTest(t)
	worker.SetRevenueCatAuthority()
	event := loadWebhookFixture(t, "transaction_completed.json")
	receipts.On("GetByID", mock.Anything, "receipt").Return(event, nil).Once()
	transactions.On("Create", mock.Anything, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "completed" && txn.UserID == "user_2abc123def456"
	})).Return(nil).Once()
	receipts.On("MarkProcessed", mock.Anything, "receipt").Return(nil).Once()
	require.NoError(t, worker.Work(context.Background(), createRiverJob(SubscriptionWebhookArgs{WebhookID: "receipt"})))
	receipts.AssertExpectations(t)
	transactions.AssertExpectations(t)
}

func TestSubscriptionWebhookRevenueCatAuthorityCannotAcknowledgeForgedReceipt(t *testing.T) {
	repo := &mockPaddleWebhookRepository{}
	repo.On("GetByID", mock.Anything, "receipt").Return(&domain.PaddleWebhookEvent{EventType: "subscription.created", SignatureValid: false}, nil).Once()
	repo.On("MarkFailed", mock.Anything, "receipt", "rejected: signature_valid=false").Return(nil).Once()
	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{PaddleWebhookRepo: repo})
	worker.SetRevenueCatAuthority()
	err := worker.Work(context.Background(), createRiverJob(SubscriptionWebhookArgs{WebhookID: "receipt"}))
	var canceled *river.JobCancelError
	require.ErrorAs(t, err, &canceled)
	repo.AssertExpectations(t)
}

func TestSubscriptionWebhookRevenueCatPaymentFailureDoesNotPublishLegacyAccess(t *testing.T) {
	worker, receipts, profiles, clerk, notifications, transactions := setupWorkerTest(t)
	worker.SetRevenueCatAuthority()
	event := loadWebhookFixture(t, "transaction_payment_failed.json")
	receipts.On("GetByID", mock.Anything, "receipt").Return(event, nil).Once()
	receipts.On("MarkProcessed", mock.Anything, "receipt").Return(nil).Once()
	transactions.On("Create", mock.Anything, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "failed" && txn.UserID == "user_2abc123def456"
	})).Return(nil).Once()
	notifications.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil).Once()
	// Returning a stale profile makes the outward Clerk write observable without
	// confusing a missing mock expectation with the forbidden mutation itself.
	profiles.On("GetUserByID", mock.Anything, "user_2abc123def456").Return(&domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk", SubscriptionTier: "premium", SubscriptionStatus: "past_due"}, nil).Maybe()
	clerk.On("UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	require.NoError(t, worker.Work(context.Background(), createRiverJob(SubscriptionWebhookArgs{WebhookID: "receipt"})))
	require.Empty(t, clerk.Calls, "Paddle must not publish legacy subscription access into Clerk")
	require.Empty(t, profiles.Calls, "billing receipts do not need legacy access fields")
	receipts.AssertExpectations(t)
	transactions.AssertExpectations(t)
	notifications.AssertExpectations(t)
}

package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAllExpectedJobKinds verifies that all expected job types have proper Kind() methods.
// This catches issues where a job kind might be misspelled or missing.
// Issue #666: Added after PlaidSyncWorker was not registered with River.
// Issue #1015: Updated to cover all registered job kinds (see TestJobArgsKindConsistency).
func TestAllExpectedJobKinds(t *testing.T) {
	testCases := []struct {
		name         string
		args         interface{ Kind() string }
		expectedKind string
	}{
		// Plaid-dependent workers
		{"PlaidSyncArgs", PlaidSyncArgs{}, "plaid_sync"},
		{"WebhookProcessingArgs", WebhookProcessingArgs{}, "webhook_processing"},
		{"InvestmentHoldingsSyncArgs", InvestmentHoldingsSyncArgs{}, "investment_holdings_sync"},
		{"InvestmentTransactionsSyncArgs", InvestmentTransactionsSyncArgs{}, "investment_transactions_sync"},
		{"ItemCleanupArgs", ItemCleanupArgs{}, "item_cleanup"},
		{"ItemCleanupCheckArgs", ItemCleanupCheckArgs{}, "item_cleanup_check"},
		// Non-Plaid workers
		{"EmailChangePropagationArgs", EmailChangePropagationArgs{}, "email_change_propagation"},
		{"AccountDeletionArgs", AccountDeletionArgs{}, "account_deletion"},
		{"ScheduledDeletionCheckArgs", ScheduledDeletionCheckArgs{}, "scheduled_deletion_check"},
		{"TrialExpirationCheckArgs", TrialExpirationCheckArgs{}, "trial_expiration_check"},
		{"PaddleReconciliationArgs", PaddleReconciliationArgs{}, "paddle_reconciliation"},
		{"RetentionCleanupCheckArgs", RetentionCleanupCheckArgs{}, "retention_cleanup_check"},
		{"DLQCleanupCheckArgs", DLQCleanupCheckArgs{}, "dlq_cleanup_check"},
		{"SubscriptionWebhookArgs", SubscriptionWebhookArgs{}, "subscription_webhook"},
		// Notification / rule workers
		{"NotificationEmailArgs", NotificationEmailArgs{}, "notification_email"},
		{"RuleEvaluationArgs", RuleEvaluationArgs{}, "rule_evaluation"},
		// Category denormalization workers
		{"EffectiveCategoryRefreshArgs", EffectiveCategoryRefreshArgs{}, "effective_category_refresh"},
	}

	for _, tc := range testCases {
		actual := tc.args.Kind()
		assert.Equal(t, tc.expectedKind, actual,
			"%s.Kind() = %q, want %q", tc.name, actual, tc.expectedKind)
	}
}

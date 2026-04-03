package posthog

// Backend analytics event name constants.
const (
	// Plaid sync events
	EventAccountSynced    = "account_synced"
	EventSyncCompleted    = "sync_completed"
	EventSyncFailed       = "sync_failed"
	EventWebhookReceived  = "webhook_received"

	// Plaid investment sync events (Issue #659)
	EventInvestmentHoldingsSynced     = "investment_holdings_synced"
	EventInvestmentTransactionsSynced = "investment_transactions_synced"
	EventInvestmentSyncFailed         = "investment_sync_failed"

	// Plaid liability sync events (Issue #910)
	EventLiabilitiesSynced   = "liabilities_synced"
	EventLiabilitySyncFailed = "liability_sync_failed"

	// Paddle payment events
	EventSubscriptionCreated    = "subscription_created"
	EventSubscriptionCancelled  = "subscription_cancelled"
	EventPaymentSucceeded       = "payment_succeeded"
	EventPaymentFailed          = "payment_failed"

	// User lifecycle events
	EventUserRegistered     = "user_registered"
	EventUserDeleted        = "user_deleted"
	EventDeletionRequested  = "deletion_requested"
	EventDeletionCancelled  = "deletion_cancelled"

	// Rule and automation events
	EventRuleTriggered              = "rule_triggered"
	EventAutoCategorizationExecuted = "auto_categorization_executed"

	// Job events
	EventDeadLetterCreated = "dead_letter_created"
)

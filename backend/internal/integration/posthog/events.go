package posthog

// Backend analytics event name constants.
const (
	// Plaid sync events
	EventAccountSynced    = "account_synced"
	EventSyncCompleted    = "sync_completed"
	EventSyncFailed       = "sync_failed"
	EventWebhookReceived  = "webhook_received"

	// Paddle payment events
	EventSubscriptionCreated    = "subscription_created"
	EventSubscriptionCancelled  = "subscription_cancelled"
	EventPaymentSucceeded       = "payment_succeeded"
	EventPaymentFailed          = "payment_failed"

	// User lifecycle events
	EventUserRegistered    = "user_registered"
	EventUserDeleted       = "user_deleted"
	EventDeletionCancelled = "deletion_cancelled"

	// Rule and automation events
	EventRuleTriggered              = "rule_triggered"
	EventAutoCategorizationExecuted = "auto_categorization_executed"

	// Job events
	EventJobCompleted      = "job_completed"
	EventJobFailed         = "job_failed"
	EventDeadLetterCreated = "dead_letter_created"
)

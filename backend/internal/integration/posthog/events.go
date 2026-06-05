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
	// Issue #1006: Paddle cleanup failure during account deletion
	EventPaddleCleanupFailed    = "paddle_cleanup_failed"

	// User lifecycle events
	EventUserRegistered     = "user_registered"
	EventUserDeleted        = "user_deleted"
	EventDeletionRequested  = "deletion_requested"
	EventDeletionCancelled  = "deletion_cancelled"
	EventEmailChanged       = "email_changed"

	// Rule and automation events
	EventRuleTriggered              = "rule_triggered"
	EventAutoCategorizationExecuted = "auto_categorization_executed"

	// Transfer suggestion events
	TransferSuggestionAccepted = "transfer_suggestion_accepted"
	TransferSuggestionRejected = "transfer_suggestion_rejected"

	// Job events
	EventDeadLetterCreated = "dead_letter_created"

	// Transaction review and recategorization events (Issue #1210)
	EventTransactionRecategorized      = "transaction_recategorized"
	EventTransactionReviewStateChanged = "transaction_review_state_changed"

	// MCP server events (Issue #1087)
	// NOTE: MCP analytics are fired exclusively by the Cloudflare Worker (apps/mcp-server/src/analytics/)
	// which has direct access to the MCP session context. The backend does not receive the PostHog
	// analytics client in its MCP-facing handlers (recipes, audit) due to the setter-injection pattern.
	// If backend-side MCP analytics are added in a follow-up, restore these constants.
	// EventMCPOAuthAuthorized = "mcp_oauth_authorized"  -- deferred: fired by Worker on Clerk token validation
	// EventMCPToolCalled      = "mcp_tool_called"       -- deferred: fired by Worker per tool invocation
	// EventMCPCodeExecuted    = "mcp_code_executed"     -- deferred: fired by Worker on execute()
	// EventMCPExecuteFailed   = "mcp_execute_failed"    -- deferred: fired by Worker on tool error
	// EventMCPSessionRevoked  = "mcp_session_revoked"   -- deferred: no revocation hook exists yet
)

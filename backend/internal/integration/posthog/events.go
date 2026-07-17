package posthog

// Backend analytics event name constants.
const (
	// Plaid sync events
	EventAccountSynced   = "account_synced"
	EventSyncCompleted   = "sync_completed"
	EventSyncFailed      = "sync_failed"
	EventWebhookReceived = "webhook_received"

	// Plaid investment sync events (Issue #659)
	EventInvestmentHoldingsSynced     = "investment_holdings_synced"
	EventInvestmentTransactionsSynced = "investment_transactions_synced"
	EventInvestmentSyncFailed         = "investment_sync_failed"

	// Plaid liability sync events (Issue #910)
	EventLiabilitiesSynced   = "liabilities_synced"
	EventLiabilitySyncFailed = "liability_sync_failed"

	// Paddle payment events
	EventSubscriptionCreated   = "subscription_created"
	EventSubscriptionCancelled = "subscription_cancelled"
	EventPaymentSucceeded      = "payment_succeeded"
	EventPaymentFailed         = "payment_failed"
	// Issue #1006: Paddle cleanup failure during account deletion
	EventPaddleCleanupFailed = "paddle_cleanup_failed"

	// Issue #898: Past-due dunning lifecycle events
	// Fired when the daily job disconnects Plaid for a past_due user (at plaid_disconnect_days threshold).
	EventPastDuePlaidDisconnected = "past_due_plaid_disconnected"
	// Fired when the daily job auto-cancels a past_due user (at grace_days threshold).
	EventPastDueAutoCancelled = "past_due_auto_cancelled"

	// User lifecycle events
	EventUserRegistered    = "user_registered"
	EventUserDeleted       = "user_deleted"
	EventDeletionRequested = "deletion_requested"
	EventDeletionCancelled = "deletion_cancelled"
	EventEmailChanged      = "email_changed"

	// Boost and automation events
	EventBoostTriggered             = "boost_triggered"
	EventAutoCategorizationExecuted = "auto_categorization_executed"

	// Transfer suggestion events
	TransferSuggestionAccepted = "transfer_suggestion_accepted"
	TransferSuggestionRejected = "transfer_suggestion_rejected"
	TransferSuggestionCanceled = "transfer_suggestion_canceled"

	// Issue #972: Savings transfer automation events
	EventTransferSuggestionMatched   = "transfer_suggestion_matched"
	EventTransferSuggestionValidated = "transfer_suggestion_validated"

	// Savings goal events (Issue #1300)
	EventSavingsGoalProgressBasisChanged = "savings_goal_progress_basis_changed"

	// Job events
	EventDeadLetterCreated = "dead_letter_created"

	// Transaction review and recategorization events (Issue #1210)
	EventTransactionRecategorized      = "transaction_recategorized"
	EventTransactionReviewStateChanged = "transaction_review_state_changed"

	// Issue #1348: Merchant analytics events
	EventMerchantOverrideSet     = "merchant_override_set"
	EventMerchantOverrideCleared = "merchant_override_cleared"
	EventMerchantsMerged         = "merchants_merged"
	EventMerchantsBulkOverridden = "merchants_bulk_overridden"

	// Household events (Issue #1367 — Phase 3: membership, invites & consent gate)
	EventHouseholdCreated        = "household_created"
	EventHouseholdDisbanded      = "household_disbanded"
	EventHouseholdInviteSent     = "household_invite_sent"
	EventHouseholdInviteAccepted = "household_invite_accepted"
	EventHouseholdMemberRemoved  = "household_member_removed"
	EventHouseholdMemberLeft     = "household_member_left"

	// Household account-sharing events (Issue #1368 — Phase 4: account sharing grants)
	EventHouseholdAccountShared = "household_account_shared"
	EventHouseholdAccountHidden = "household_account_hidden"

	// Household offboarding & lifecycle events (Issue #1371 — Phase 7)
	EventHouseholdMemberAutoOffboarded = "household_member_auto_offboarded"
	EventHouseholdMembershipPurged     = "household_membership_purged"
	EventHouseholdForceDisbanded       = "household_force_disbanded"
	// EventHouseholdOverCapacityEntered fires the first time a household's active
	// member count exceeds its owner's current plan seat count. Only fired from the
	// Task 6, Part B webhook fast-path detection (subscription_webhook.go's
	// reconcileOverCapacity) — the Task 6 daily backstop worker
	// (household_over_capacity_check.go) has no analytics calls at all; it flags/
	// clears the over-capacity state silently.
	EventHouseholdOverCapacityEntered = "household_over_capacity_entered"

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

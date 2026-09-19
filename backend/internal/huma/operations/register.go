// Package operations provides Huma operation handlers for the BoostedBudget API.
// This file provides a central registration function for OpenAPI spec generation.
package operations

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RegisterAllRoutesForSpec registers all API routes with dummy handlers for OpenAPI spec generation.
// This allows generating the complete OpenAPI spec without needing actual service dependencies.
// The handlers are not called during spec generation - Huma only needs the type signatures.
func RegisterAllRoutesForSpec(api huma.API) {
	// Create minimal tier config to avoid panic in SubscriptionTiersHandler
	minimalTierConfig := &config.TierConfig{
		Tiers: map[domain.TierName]domain.SubscriptionTier{
			"free": {Name: "free", DisplayName: "Free"},
		},
	}

	// Create minimal region config for spec generation
	minimalRegionConfig := &config.RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "iad",
		Regions:       map[string]config.RegionDefinition{"iad": {DisplayName: "USA (Virginia)"}},
	}

	// Create handlers with nil dependencies
	// These handlers won't be called - they're just needed for type signatures
	healthHandler := &HealthHandler{}
	subscriptionTiersHandler := NewSubscriptionTiersHandler(minimalTierConfig)
	regionsHandler := NewRegionsHandler(minimalRegionConfig)
	tagHandler := &TagHandler{}
	transactionHandler := &TransactionHandler{}
	categoryHandler := &CategoryHandler{}
	categorizationRuleHandler := &CategorizationRuleHandler{}
	ruleExecutionHandler := &RuleExecutionHandler{}
	transactionAnnotationHistoryHandler := &TransactionAnnotationHistoryHandler{}
	notificationHandler := &NotificationHandler{}
	dashboardHandler := &DashboardHandler{}
	analyticsHandler := &AnalyticsHandler{}
	dashboardPreferencesHandler := &DashboardPreferencesHandler{}
	budgetHandler := &BudgetHandler{}
	savingsGoalHandler := &SavingsGoalHandler{}
	plaidHandler := &PlaidHandler{}
	// Issue #1609: the reconnect-flow handler (separate from PlaidHandler — see plaid_reconnect.go).
	plaidReconnectHandler := &PlaidReconnectHandler{}
	profileHandler := &ProfileHandler{}
	privacyHandler := &PrivacyHandler{}
	postHogSupportIdentityHandler := &PostHogSupportIdentityHandler{}
	// Issue #1587: GDPR data export
	dataExportHandler := &DataExportHandler{}
	adminHandler := &AdminHandler{}
	categoryPreviewHandler := &CategoryPreviewHandler{}
	deletionHandler := &DeletionHandler{}
	plaidWebhookHandler := &PlaidWebhookHandler{}

	// Boosts (Issues #34-38)
	boostHandler := &BoostHandler{}

	// Issue #45, #46: Paddle payment integration
	paddleCheckoutHandler := &PaddleCheckoutHandler{}
	paddleWebhookHandler := &PaddleWebhookHandler{}

	// Issue #171: Stable user account references
	userAccountHandler := &UserAccountHandler{}

	// Issue #821: QA auth bypass (zero-value OK—handler not called during spec generation)
	qaBypassHandler := &QAAuthBypassHandler{}

	// Issue #952: Notification Center — Transfer Suggestion API
	transferSuggestionHandler := &TransferSuggestionHandler{}

	// Issue #972: Savings impact routes (C1: public aggregate, C2: per-user impact)
	savingsImpactHandler := &SavingsImpactHandler{}

	// Issue #1087: MCP saved recipes
	recipeHandler := &RecipeHandler{}

	// Issue #1087: MCP audit log
	mcpAuditHandler := &MCPAuditHandler{}
	// Issue #1650: consent-gated MCP product telemetry
	mcpToolTelemetryHandler := &MCPToolTelemetryHandler{}

	// Issue #138: Investment portfolio endpoints (8 ops: 4 core + 4 analytics)
	investmentHandler := &InvestmentHandler{}

	// Issue #1569: retained holdings history for a closed account (1 op)
	holdingHistoryHandler := &HoldingHistoryHandler{}

	// Issue #1569: retained per-account balance history for a closed account (1 op)
	accountBalanceHistoryHandler := &AccountBalanceHistoryHandler{}

	// Issue #1300 M20: Savings goal contribution API (D6 source-a manual log + confirm + delete)
	contributionHandler := &ContributionHandler{}

	// Issue #957: Merchant name handling
	merchantHandler := &MerchantHandler{}

	// Issue #1366: Household entitlement summary
	householdEntitlementHandler := &HouseholdEntitlementHandler{}

	// Issue #1367: Household membership, invites & consent gate (Phase 3)
	householdHandler := &HouseholdHandler{}

	// Issue #1368: Household account sharing grants (Phase 4)
	householdShareHandler := &HouseholdShareHandler{}

	// Register all routes
	//
	// Issue #1514: /metrics is deliberately NOT registered here. It moved onto an
	// internal-only listener (see cmd/server/main.go's internalHumaAPI / internal_metrics.go)
	// and is not part of the public API surface anymore, so it must not be advertised in the
	// spec generated from this function — doing so previously produced an
	// unauthenticated-looking public GET /metrics in api/openapi.yaml (and a generated
	// frontend client for it) that does not exist at runtime.
	RegisterHealthRoutes(api, healthHandler)
	RegisterSubscriptionTiersRoutes(api, subscriptionTiersHandler)
	RegisterTagRoutes(api, tagHandler)
	RegisterTransactionRoutes(api, transactionHandler)
	RegisterCategoryRoutes(api, categoryHandler)
	RegisterCategorizationRuleRoutes(api, categorizationRuleHandler)
	// Issue #1569 Task 5 (#1572): the automation audit trail's read endpoint.
	RegisterRuleExecutionRoutes(api, ruleExecutionHandler)

	// Issue #1569 Task 6 (#1579): the annotation trail's read endpoint.
	RegisterTransactionAnnotationHistoryRoutes(api, transactionAnnotationHistoryHandler)
	RegisterNotificationRoutes(api, notificationHandler)
	RegisterDashboardRoutes(api, dashboardHandler)
	RegisterAnalyticsRoutes(api, analyticsHandler)
	RegisterDashboardPreferencesRoutes(api, dashboardPreferencesHandler)
	RegisterBudgetRoutes(api, budgetHandler)
	RegisterSavingsGoalRoutes(api, savingsGoalHandler)
	RegisterPlaidRoutes(api, plaidHandler)
	RegisterPlaidReconnectRoutes(api, plaidReconnectHandler)
	RegisterProfileRoutes(api, profileHandler)
	RegisterPrivacyRoutes(api, privacyHandler)
	RegisterPostHogSupportIdentityRoutes(api, postHogSupportIdentityHandler)
	RegisterDataExportRoutes(api, dataExportHandler)
	RegisterAdminRoutes(api, adminHandler)
	RegisterCategoryPreviewRoutes(api, categoryPreviewHandler)
	RegisterDeletionRoutes(api, deletionHandler)
	RegisterPlaidWebhookRoutes(api, plaidWebhookHandler)

	// Boosts (Issues #34-38)
	RegisterBoostRoutes(api, boostHandler)

	// Issue #171: Stable user account references
	RegisterUserAccountRoutes(api, userAccountHandler)

	// Issue #45, #46: Paddle payment integration
	RegisterPaddleCheckoutRoutes(api, paddleCheckoutHandler)
	RegisterPaddleWebhookRoutes(api, paddleWebhookHandler)
	RegisterRevenueCatWebhookRoutes(api, &RevenueCatWebhookHandler{})
	RegisterRevenueCatSubscriptionRoutes(api, &RevenueCatSubscriptionHandler{})
	RegisterAdminRevenueCatRoutes(api, adminHandler)

	// Issue #821: QA auth bypass
	RegisterQAAuthBypassRoutes(api, qaBypassHandler)

	// Issue #974: Dynamic region configuration
	RegisterRegionRoutes(api, regionsHandler)

	// Issue #952: Notification Center — Transfer Suggestion API
	RegisterTransferSuggestionRoutes(api, transferSuggestionHandler)

	// Issue #972: Savings impact (C1: public aggregate, C2: per-user impact)
	RegisterSavingsImpactRoutes(api, savingsImpactHandler)

	// Issue #1087: MCP saved recipes
	RegisterRecipeRoutes(api, recipeHandler)

	// Issue #1087: MCP audit log
	RegisterMCPAuditRoutes(api, mcpAuditHandler)
	RegisterMCPToolTelemetryRoutes(api, mcpToolTelemetryHandler)

	// Issue #138: Investment portfolio (4 core + 4 analytics operations)
	RegisterInvestmentRoutes(api, investmentHandler)

	// Issue #1569: retained holdings history (historical universe — see holding_history.go)
	RegisterHoldingHistoryRoutes(api, holdingHistoryHandler)

	// Issue #1569: retained per-account balance history (historical universe — see
	// account_balance_history.go)
	RegisterAccountBalanceHistoryRoutes(api, accountBalanceHistoryHandler)

	// Issue #1300 M20: Savings goal contribution API
	RegisterContributionRoutes(api, contributionHandler)

	// Issue #957: Merchant name handling
	RegisterMerchantRoutes(api, merchantHandler)

	// Issue #1366: Household entitlement summary
	RegisterHouseholdEntitlementRoutes(api, householdEntitlementHandler)
	RegisterHouseholdRoutes(api, householdHandler)

	// Issue #1368: Household account sharing grants (Phase 4)
	RegisterHouseholdShareRoutes(api, householdShareHandler)
}

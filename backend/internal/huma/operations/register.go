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

	// Create handlers with nil dependencies
	// These handlers won't be called - they're just needed for type signatures
	healthHandler := &HealthHandler{Version: "1.0.0"}
	metricsHandler := &MetricsHandler{}
	subscriptionTiersHandler := NewSubscriptionTiersHandler(minimalTierConfig)
	tagHandler := &TagHandler{}
	transactionHandler := &TransactionHandler{}
	categoryHandler := &CategoryHandler{}
	categorizationRuleHandler := &CategorizationRuleHandler{}
	notificationHandler := &NotificationHandler{}
	dashboardHandler := &DashboardHandler{}
	analyticsHandler := &AnalyticsHandler{}
	dashboardPreferencesHandler := &DashboardPreferencesHandler{}
	budgetHandler := &BudgetHandler{}
	savingsGoalHandler := &SavingsGoalHandler{}
	plaidHandler := &PlaidHandler{}
	profileHandler := &ProfileHandler{}
	privacyHandler := &PrivacyHandler{}
	adminHandler := &AdminHandler{}
	categoryPreviewHandler := &CategoryPreviewHandler{}
	deletionHandler := &DeletionHandler{}
	plaidWebhookHandler := &PlaidWebhookHandler{}

	// Automation Rules (Issues #34-38)
	automationRuleHandler := &AutomationRuleHandler{}

	// Issue #45, #46: Paddle payment integration
	paddleCheckoutHandler := &PaddleCheckoutHandler{}
	paddleWebhookHandler := &PaddleWebhookHandler{}

	// Issue #171: Stable user account references
	userAccountHandler := &UserAccountHandler{}

	// Issue #821: QA auth bypass (zero-value OK—handler not called during spec generation)
	qaBypassHandler := &QAAuthBypassHandler{}

	// Register all routes
	RegisterHealthRoutes(api, healthHandler)
	RegisterMetricsRoutes(api, metricsHandler)
	RegisterSubscriptionTiersRoutes(api, subscriptionTiersHandler)
	RegisterTagRoutes(api, tagHandler)
	RegisterTransactionRoutes(api, transactionHandler)
	RegisterCategoryRoutes(api, categoryHandler)
	RegisterCategorizationRuleRoutes(api, categorizationRuleHandler)
	RegisterNotificationRoutes(api, notificationHandler)
	RegisterDashboardRoutes(api, dashboardHandler)
	RegisterAnalyticsRoutes(api, analyticsHandler)
	RegisterDashboardPreferencesRoutes(api, dashboardPreferencesHandler)
	RegisterBudgetRoutes(api, budgetHandler)
	RegisterSavingsGoalRoutes(api, savingsGoalHandler)
	RegisterPlaidRoutes(api, plaidHandler)
	RegisterProfileRoutes(api, profileHandler)
	RegisterPrivacyRoutes(api, privacyHandler)
	RegisterAdminRoutes(api, adminHandler)
	RegisterCategoryPreviewRoutes(api, categoryPreviewHandler)
	RegisterDeletionRoutes(api, deletionHandler)
	RegisterPlaidWebhookRoutes(api, plaidWebhookHandler)

	// Automation Rules (Issues #34-38)
	RegisterAutomationRuleRoutes(api, automationRuleHandler)

	// Issue #171: Stable user account references
	RegisterUserAccountRoutes(api, userAccountHandler)

	// Issue #45, #46: Paddle payment integration
	RegisterPaddleCheckoutRoutes(api, paddleCheckoutHandler)
	RegisterPaddleWebhookRoutes(api, paddleWebhookHandler)

	// Issue #821: QA auth bypass
	RegisterQAAuthBypassRoutes(api, qaBypassHandler)
}

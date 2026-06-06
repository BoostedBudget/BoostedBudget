package domain

// MCPOperationClass classifies an operation's capability for the MCP server.
type MCPOperationClass string

const (
	MCPClassRead     MCPOperationClass = "read"
	MCPClassWrite    MCPOperationClass = "write"
	MCPClassExcluded MCPOperationClass = "excluded"
)

// MCPOperationManifest classifies a known operationId.
// This mirrors the TypeScript capability manifest in apps/mcp-server.
// The Go copy is used to validate recipe specs at the service layer —
// recipes may only contain read-class operations in v1.
var MCPOperationManifest = map[string]MCPOperationClass{
	// Analytics — all read
	"getCashFlowHistory":          MCPClassRead,
	"getCashFlowSummary":          MCPClassRead,
	"getSpendingByCategory":       MCPClassRead,
	"getSpendingByMerchant":       MCPClassRead,
	"getSpendingTrends":           MCPClassRead,
	"getCategoryTotalForPeriod":   MCPClassRead,
	"getTopExpense":               MCPClassRead,
	"getAverageMonthlySpend":              MCPClassRead,
	"getRecurringSubscriptions":           MCPClassRead,
	"getSavingsRateForPeriod":             MCPClassRead,
	"getMonthOverMonthSpendingChange":     MCPClassRead,
	"getLargestTransactionsForPeriod":     MCPClassRead,
	"getCategoryBreakdownForMerchant":     MCPClassRead,
	"getIncomeSourceBreakdown":            MCPClassRead,
	"getUnusualSpend":                     MCPClassRead,
	"getNetWorthTrend":                    MCPClassRead,
	"getBudgetVsActual":                   MCPClassRead,
	"getFinancialSummary":                 MCPClassRead,
	// Accounts
	"getAccounts":      MCPClassRead,
	"reorderAccounts":  MCPClassWrite,
	"updateAccount":    MCPClassWrite,
	"deleteAccount":    MCPClassExcluded,
	"disconnectInstitution": MCPClassExcluded,
	// Automation Rules — reads only (writes excluded)
	"getAutomationRules":    MCPClassRead,
	"getAutomationRuleByID": MCPClassRead,
	"getRuleExecutions":     MCPClassRead,
	// Auto-Categorization — reads only
	"getUserRules": MCPClassRead,
	"getRuleByID":  MCPClassRead,
	// Budgets
	"getBudgets":           MCPClassRead,
	"getAvailableCategories": MCPClassRead,
	"getCurrentBudget":     MCPClassRead,
	"getBudget":            MCPClassRead,
	"getCategoryTransactions": MCPClassRead,
	"createBudget":         MCPClassWrite,
	"updateBudget":         MCPClassWrite,
	"deleteBudget":         MCPClassWrite,
	"addBudgetCategory":    MCPClassWrite,
	"updateBudgetCategory": MCPClassWrite,
	"deleteBudgetCategory": MCPClassWrite,
	"syncBudget":           MCPClassWrite,
	"previewCategoryMatches": MCPClassRead,
	// Categories
	"getUserCategories":       MCPClassRead,
	"getPlaidCategories":      MCPClassRead,
	"getPlaidDetailedCategories": MCPClassRead,
	"getPlaidPrimaryCategories": MCPClassRead,
	"getCategoryByID":         MCPClassRead,
	"getCategoryUsage":        MCPClassRead,
	"createCategory":          MCPClassWrite,
	"updateCategory":          MCPClassWrite,
	"deleteCategory":          MCPClassWrite,
	// Dashboard — reads; layout writes excluded
	"getAccountTypes":         MCPClassRead,
	"getBalanceHistory":       MCPClassRead,
	"getBalanceSummary":       MCPClassRead,
	"getNetWorth":             MCPClassRead,
	"getNetWorthHistory":      MCPClassRead,
	"getDashboardPreferences": MCPClassRead,
	"getDefaultLayout":        MCPClassRead,
	"getWidgetTypes":          MCPClassRead,
	// Notifications
	"getNotifications":           MCPClassRead,
	"getUnreadNotificationCount": MCPClassRead,
	"markNotificationAsRead":     MCPClassWrite,
	"markAllNotificationsAsRead": MCPClassWrite,
	// Savings Goals
	"getSavingsGoals":       MCPClassRead,
	"getActiveSavingsGoals": MCPClassRead,
	"getSavingsGoal":        MCPClassRead,
	"createSavingsGoal":     MCPClassWrite,
	"updateSavingsGoal":     MCPClassWrite,
	"deleteSavingsGoal":     MCPClassWrite,
	// Subscription — reads only; writes excluded
	"get-current-subscription": MCPClassRead,
	"getSubscriptionTiers":     MCPClassRead,
	"get-billing-portal-url":   MCPClassRead,
	// Tags
	"getUserTags": MCPClassRead,
	"getTagByID":  MCPClassRead,
	"createTag":   MCPClassWrite,
	"updateTag":   MCPClassWrite,
	"deleteTag":   MCPClassWrite,
	// Transactions
	"getTransactions":              MCPClassRead,
	"getTransactionByID":           MCPClassRead,
	"getTransactionReviewSummary":  MCPClassRead,
	"updateTransactionCategory":    MCPClassWrite,
	"updateTransactionTags":        MCPClassWrite,
	"setTransactionReviewed":       MCPClassWrite,
	"bulkSetTransactionReviewed":   MCPClassWrite,
	"bulkUpdateTransactionCategory": MCPClassWrite,
	// Transfer Suggestions
	"getTransferSuggestions":            MCPClassRead,
	"acceptTransferSuggestion":          MCPClassWrite,
	"rejectTransferSuggestion":          MCPClassWrite,
	"confirmTransferSuggestionMatch":    MCPClassWrite, // #972: idempotent write
	// Savings Impact — #972
	"getSavingsImpactAggregate": MCPClassRead,
	"getSavingsImpactMe":        MCPClassRead,
	// User Accounts
	"getUserAccounts":    MCPClassRead,
	"getUserAccountById": MCPClassRead,
	"updateUserAccount":  MCPClassWrite,
	// Profile/Privacy — reads only; writes excluded
	"getPrivacySettings": MCPClassRead,
	"getUserProfile":     MCPClassRead,
	// Regions
	"getRegions": MCPClassRead,
}

// MCPOperationAllowed returns true if the operationId is allowed through MCP
// (present in the manifest and not excluded).
func MCPOperationAllowed(operationID string) bool {
	class, ok := MCPOperationManifest[operationID]
	return ok && class != MCPClassExcluded
}

// MCPOperationIsRead returns true if the operation is a read-class operation.
func MCPOperationIsRead(operationID string) bool {
	return MCPOperationManifest[operationID] == MCPClassRead
}

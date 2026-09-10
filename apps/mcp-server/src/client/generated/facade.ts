/**
 * BoostedBudget API facade for Cloudflare Workers.
 * AUTO-GENERATED — do not edit manually.
 * Run: bun run scripts/generate-facade.ts
 *
 * Contains only allowed operations (excluded ops are absent).
 * All calls are routed through the fetch mutator which injects
 * Authorization + X-Origin-Secret per request.
 */

import type { FetchMutator } from '../fetch-mutator'

export function createApiClient(mutator: FetchMutator) {
  return {
    accounts: {
      /** GET /api/v1/accounts */
      getAccounts(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/accounts', method: 'GET', params, signal })
      },
      /** PATCH /api/v1/accounts/reorder */
      reorderAccounts(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/accounts/reorder', method: 'PATCH', data, signal })
      },
      /** PATCH /api/v1/accounts/{id} */
      updateAccount(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/accounts/${id}`, method: 'PATCH', data, signal })
      },
      /** GET /api/v1/accounts/{id}/balance-history */
      getAccountBalanceHistory(id: string, params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/accounts/${id}/balance-history`, method: 'GET', params, signal })
      },
    } as const,
    analytics: {
      /** GET /api/v1/analytics/average-monthly-spend */
      getAverageMonthlySpend(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/average-monthly-spend', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/budget-vs-actual */
      getBudgetVsActual(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/budget-vs-actual', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/cash-flow-history */
      getCashFlowHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/cash-flow-history', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/cash-flow-summary */
      getCashFlowSummary(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/cash-flow-summary', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/category-hierarchy */
      getCategoryHierarchy(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/category-hierarchy', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/category-total */
      getCategoryTotalForPeriod(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/category-total', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/financial-summary */
      getFinancialSummary(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/financial-summary', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/income-sources */
      getIncomeSourceBreakdown(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/income-sources', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/largest-transactions */
      getLargestTransactionsForPeriod(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/largest-transactions', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/merchant-category-breakdown */
      getCategoryBreakdownForMerchant(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/merchant-category-breakdown', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/month-over-month */
      getMonthOverMonthSpendingChange(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/month-over-month', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/net-worth-trend */
      getNetWorthTrend(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/net-worth-trend', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/recurring-subscriptions */
      getRecurringSubscriptions(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/recurring-subscriptions', method: 'GET', signal })
      },
      /** GET /api/v1/analytics/savings-rate */
      getSavingsRateForPeriod(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/savings-rate', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/spending-by-category */
      getSpendingByCategory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/spending-by-category', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/spending-by-merchant */
      getSpendingByMerchant(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/spending-by-merchant', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/spending-trends */
      getSpendingTrends(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/spending-trends', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/top-expense */
      getTopExpense(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/top-expense', method: 'GET', params, signal })
      },
      /** GET /api/v1/analytics/unusual-spend */
      getUnusualSpend(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/analytics/unusual-spend', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/portfolio/allocation */
      getPortfolioAllocationBreakdown(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/allocation', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/portfolio/gain-loss */
      getInvestmentGainLossSummary(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/gain-loss', method: 'GET', signal })
      },
      /** GET /api/v1/investments/portfolio/income */
      getInvestmentIncomeSummary(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/income', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/portfolio/top-movers */
      getTopMovers(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/top-movers', method: 'GET', params, signal })
      },
    } as const,
    autoCategorization: {
      /** GET /api/v1/auto-categorization */
      getUserRules(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/auto-categorization', method: 'GET', signal })
      },
      /** POST /api/v1/auto-categorization */
      createRule(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/auto-categorization', method: 'POST', data, signal })
      },
      /** GET /api/v1/auto-categorization/condition-schema */
      getConditionSchema(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/auto-categorization/condition-schema', method: 'GET', signal })
      },
      /** POST /api/v1/auto-categorization/preview */
      previewRule(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/auto-categorization/preview', method: 'POST', data, signal })
      },
      /** GET /api/v1/auto-categorization/{id} */
      getRuleByID(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/auto-categorization/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/auto-categorization/{id} */
      updateRule(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/auto-categorization/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/auto-categorization/{id} */
      deleteRule(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/auto-categorization/${id}`, method: 'DELETE', signal })
      },
      /** POST /api/v1/auto-categorization/{id}/toggle */
      toggleRule(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/auto-categorization/${id}/toggle`, method: 'POST', data, signal })
      },
      /** GET /api/v1/rule-executions */
      getUserRuleExecutions(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/rule-executions', method: 'GET', params, signal })
      },
    } as const,
    boosts: {
      /** GET /api/v1/boost-executions */
      getUserBoostExecutions(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/boost-executions', method: 'GET', params, signal })
      },
      /** GET /api/v1/boosts */
      getBoosts(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/boosts', method: 'GET', params, signal })
      },
      /** GET /api/v1/boosts/{id} */
      getBoostByID(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/boosts/${id}`, method: 'GET', signal })
      },
      /** GET /api/v1/boosts/{id}/executions */
      getBoostExecutions(id: string, params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/boosts/${id}/executions`, method: 'GET', params, signal })
      },
    } as const,
    budget: {
      /** POST /api/v1/budgets/categories/preview */
      previewCategoryMatches(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/budgets/categories/preview', method: 'POST', data, signal })
      },
    } as const,
    budgets: {
      /** GET /api/v1/budgets */
      getBudgets(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/budgets', method: 'GET', signal })
      },
      /** POST /api/v1/budgets */
      createBudget(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/budgets', method: 'POST', data, signal })
      },
      /** GET /api/v1/budgets/categories/available */
      getAvailableCategories(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/budgets/categories/available', method: 'GET', signal })
      },
      /** GET /api/v1/budgets/current */
      getCurrentBudget(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/budgets/current', method: 'GET', signal })
      },
      /** GET /api/v1/budgets/{id} */
      getBudget(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/budgets/{id} */
      updateBudget(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/budgets/{id} */
      deleteBudget(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}`, method: 'DELETE', signal })
      },
      /** POST /api/v1/budgets/{id}/categories */
      addBudgetCategory(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}/categories`, method: 'POST', data, signal })
      },
      /** PUT /api/v1/budgets/{id}/categories/{categoryId} */
      updateBudgetCategory(id: string, categoryId: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}/categories/${categoryId}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/budgets/{id}/categories/{categoryId} */
      deleteBudgetCategory(id: string, categoryId: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}/categories/${categoryId}`, method: 'DELETE', signal })
      },
      /** GET /api/v1/budgets/{id}/categories/{categoryId}/transactions */
      getCategoryTransactions(id: string, categoryId: string, params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}/categories/${categoryId}/transactions`, method: 'GET', params, signal })
      },
      /** POST /api/v1/budgets/{id}/sync */
      syncBudget(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/budgets/${id}/sync`, method: 'POST', data, signal })
      },
    } as const,
    categories: {
      /** GET /api/v1/categories */
      getUserCategories(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/categories', method: 'GET', signal })
      },
      /** POST /api/v1/categories */
      createCategory(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/categories', method: 'POST', data, signal })
      },
      /** GET /api/v1/categories/plaid */
      getPlaidCategories(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/categories/plaid', method: 'GET', signal })
      },
      /** GET /api/v1/categories/plaid-detailed */
      getPlaidDetailedCategories(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/categories/plaid-detailed', method: 'GET', signal })
      },
      /** GET /api/v1/categories/plaid-primaries */
      getPlaidPrimaryCategories(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/categories/plaid-primaries', method: 'GET', signal })
      },
      /** GET /api/v1/categories/{id} */
      getCategoryByID(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/categories/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/categories/{id} */
      updateCategory(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/categories/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/categories/{id} */
      deleteCategory(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/categories/${id}`, method: 'DELETE', signal })
      },
      /** GET /api/v1/categories/{id}/usage */
      getCategoryUsage(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/categories/${id}/usage`, method: 'GET', signal })
      },
    } as const,
    dashboard: {
      /** GET /api/v1/dashboard/account-types */
      getAccountTypes(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/account-types', method: 'GET', signal })
      },
      /** GET /api/v1/dashboard/balance-history */
      getBalanceHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/balance-history', method: 'GET', params, signal })
      },
      /** GET /api/v1/dashboard/balance-summary */
      getBalanceSummary(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/balance-summary', method: 'GET', params, signal })
      },
      /** GET /api/v1/dashboard/default-layout */
      getDefaultLayout(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/default-layout', method: 'GET', signal })
      },
      /** GET /api/v1/dashboard/net-worth */
      getNetWorth(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/net-worth', method: 'GET', params, signal })
      },
      /** GET /api/v1/dashboard/net-worth-history */
      getNetWorthHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/net-worth-history', method: 'GET', params, signal })
      },
      /** GET /api/v1/dashboard/preferences */
      getDashboardPreferences(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/preferences', method: 'GET', signal })
      },
      /** GET /api/v1/dashboard/widget-types */
      getWidgetTypes(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/dashboard/widget-types', method: 'GET', signal })
      },
    } as const,
    household: {
      /** GET /api/v1/households/invites/{token} */
      getHouseholdInvitePreview(token: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/households/invites/${token}`, method: 'GET', signal })
      },
      /** GET /api/v1/households/me */
      getMyHousehold(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/households/me', method: 'GET', signal })
      },
      /** GET /api/v1/households/me/account-shares */
      listMyAccountShares(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/households/me/account-shares', method: 'GET', signal })
      },
      /** GET /api/v1/households/me/entitlement */
      getHouseholdEntitlement(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/households/me/entitlement', method: 'GET', signal })
      },
      /** GET /api/v1/households/me/invites */
      listHouseholdInvites(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/households/me/invites', method: 'GET', signal })
      },
      /** GET /api/v1/households/me/members */
      listHouseholdMembers(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/households/me/members', method: 'GET', signal })
      },
    } as const,
    investments: {
      /** GET /api/v1/investments/holdings */
      getHoldings(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/holdings', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/holdings/history */
      getAccountHoldingsHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/holdings/history', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/investment-transactions */
      getInvestmentTransactions(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/investment-transactions', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/portfolio/history */
      getPortfolioHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/history', method: 'GET', params, signal })
      },
      /** GET /api/v1/investments/portfolio/summary */
      getPortfolioSummary(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/investments/portfolio/summary', method: 'GET', signal })
      },
    } as const,
    mcpRecipes: {
      /** GET /api/v1/mcp/recipes */
      listMCPRecipes(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/mcp/recipes', method: 'GET', signal })
      },
      /** POST /api/v1/mcp/recipes */
      createMCPRecipe(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/mcp/recipes', method: 'POST', data, signal })
      },
      /** GET /api/v1/mcp/recipes/{id} */
      getMCPRecipe(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/mcp/recipes/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/mcp/recipes/{id} */
      updateMCPRecipe(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/mcp/recipes/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/mcp/recipes/{id} */
      deleteMCPRecipe(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/mcp/recipes/${id}`, method: 'DELETE', signal })
      },
      /** POST /api/v1/mcp/recipes/{id}/run */
      runMCPRecipe(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/mcp/recipes/${id}/run`, method: 'POST', data, signal })
      },
    } as const,
    merchants: {
      /** GET /api/v1/merchants */
      getMerchants(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/merchants', method: 'GET', signal })
      },
      /** GET /api/v1/merchants/duplicate-suggestions */
      getMerchantDuplicateSuggestions(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/merchants/duplicate-suggestions', method: 'GET', signal })
      },
      /** POST /api/v1/merchants/merge */
      mergeMerchants(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/merchants/merge', method: 'POST', data, signal })
      },
      /** GET /api/v1/merchants/overrides */
      getMerchantOverrides(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/merchants/overrides', method: 'GET', signal })
      },
      /** POST /api/v1/merchants/overrides/bulk */
      bulkSetMerchantOverrides(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/merchants/overrides/bulk', method: 'POST', data, signal })
      },
      /** PUT /api/v1/merchants/{merchantId}/override */
      setMerchantOverride(merchantId: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/merchants/${merchantId}/override`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/merchants/{merchantId}/override */
      clearMerchantOverride(merchantId: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/merchants/${merchantId}/override`, method: 'DELETE', signal })
      },
    } as const,
    notifications: {
      /** GET /api/v1/notifications */
      getNotifications(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/notifications', method: 'GET', params, signal })
      },
      /** PATCH /api/v1/notifications/read-all */
      markAllNotificationsAsRead(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/notifications/read-all', method: 'PATCH', data, signal })
      },
      /** GET /api/v1/notifications/unread/count */
      getUnreadNotificationCount(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/notifications/unread/count', method: 'GET', signal })
      },
      /** PATCH /api/v1/notifications/{id}/read */
      markNotificationAsRead(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/notifications/${id}/read`, method: 'PATCH', data, signal })
      },
    } as const,
    privacy: {
      /** GET /api/v1/user/privacy */
      getPrivacySettings(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/user/privacy', method: 'GET', signal })
      },
    } as const,
    profile: {
      /** GET /api/v1/user/profile */
      getUserProfile(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/user/profile', method: 'GET', signal })
      },
    } as const,
    regions: {
      /** GET /api/v1/regions */
      getRegions(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/regions', method: 'GET', signal })
      },
    } as const,
    savingsGoals: {
      /** GET /api/v1/savings-goals */
      getSavingsGoals(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-goals', method: 'GET', signal })
      },
      /** POST /api/v1/savings-goals */
      createSavingsGoal(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-goals', method: 'POST', data, signal })
      },
      /** GET /api/v1/savings-goals/active */
      getActiveSavingsGoals(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-goals/active', method: 'GET', signal })
      },
      /** GET /api/v1/savings-goals/deleted */
      getDeletedSavingsGoals(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-goals/deleted', method: 'GET', signal })
      },
      /** GET /api/v1/savings-goals/deleted/{goalID}/contributions */
      listDeletedSavingsGoalContributions(goalID: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/deleted/${goalID}/contributions`, method: 'GET', signal })
      },
      /** GET /api/v1/savings-goals/progress-summary */
      getSavingsProgressSummary(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-goals/progress-summary', method: 'GET', signal })
      },
      /** GET /api/v1/savings-goals/{goalID}/contributions */
      listSavingsGoalContributions(goalID: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${goalID}/contributions`, method: 'GET', signal })
      },
      /** POST /api/v1/savings-goals/{goalID}/contributions */
      logSavingsGoalContribution(goalID: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${goalID}/contributions`, method: 'POST', data, signal })
      },
      /** POST /api/v1/savings-goals/{goalID}/contributions/{id}/confirm */
      confirmSavingsGoalContribution(goalID: string, id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${goalID}/contributions/${id}/confirm`, method: 'POST', data, signal })
      },
      /** POST /api/v1/savings-goals/{goalID}/contributions/{id}/reject */
      rejectSavingsGoalContribution(goalID: string, id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${goalID}/contributions/${id}/reject`, method: 'POST', data, signal })
      },
      /** GET /api/v1/savings-goals/{id} */
      getSavingsGoal(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/savings-goals/{id} */
      updateSavingsGoal(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/savings-goals/{id} */
      deleteSavingsGoal(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/savings-goals/${id}`, method: 'DELETE', signal })
      },
    } as const,
    savingsimpact: {
      /** GET /api/v1/savings-impact/aggregate */
      getSavingsImpactAggregate(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-impact/aggregate', method: 'GET', signal })
      },
      /** GET /api/v1/savings-impact/me */
      getSavingsImpactMe(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/savings-impact/me', method: 'GET', signal })
      },
    } as const,
    subscription: {
      /** GET /api/v1/subscription/billing-portal */
      getBillingPortalUrl(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/subscription/billing-portal', method: 'GET', signal })
      },
      /** GET /api/v1/subscription/current */
      getCurrentSubscription(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/subscription/current', method: 'GET', signal })
      },
      /** GET /api/v1/subscription/tiers */
      getSubscriptionTiers(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/subscription/tiers', method: 'GET', signal })
      },
    } as const,
    tags: {
      /** GET /api/v1/tags */
      getUserTags(signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/tags', method: 'GET', signal })
      },
      /** POST /api/v1/tags */
      createTag(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/tags', method: 'POST', data, signal })
      },
      /** GET /api/v1/tags/{id} */
      getTagByID(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/tags/${id}`, method: 'GET', signal })
      },
      /** PUT /api/v1/tags/{id} */
      updateTag(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/tags/${id}`, method: 'PUT', data, signal })
      },
      /** DELETE /api/v1/tags/{id} */
      deleteTag(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/tags/${id}`, method: 'DELETE', signal })
      },
    } as const,
    transactions: {
      /** GET /api/v1/transaction-annotation-history */
      getUserTransactionAnnotationHistory(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transaction-annotation-history', method: 'GET', params, signal })
      },
      /** GET /api/v1/transactions */
      getTransactions(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transactions', method: 'GET', params, signal })
      },
      /** PUT /api/v1/transactions/category */
      bulkUpdateTransactionCategory(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transactions/category', method: 'PUT', data, signal })
      },
      /** PATCH /api/v1/transactions/review */
      bulkSetTransactionReviewed(data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transactions/review', method: 'PATCH', data, signal })
      },
      /** GET /api/v1/transactions/review-summary */
      getTransactionReviewSummary(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transactions/review-summary', method: 'GET', params, signal })
      },
      /** GET /api/v1/transactions/{id} */
      getTransactionByID(id: string, params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transactions/${id}`, method: 'GET', params, signal })
      },
      /** PUT /api/v1/transactions/{id}/category */
      updateTransactionCategory(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transactions/${id}/category`, method: 'PUT', data, signal })
      },
      /** PATCH /api/v1/transactions/{id}/review */
      setTransactionReviewed(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transactions/${id}/review`, method: 'PATCH', data, signal })
      },
      /** PUT /api/v1/transactions/{id}/tags */
      updateTransactionTags(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transactions/${id}/tags`, method: 'PUT', data, signal })
      },
    } as const,
    transfersuggestions: {
      /** GET /api/v1/transfer-suggestions */
      getTransferSuggestions(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/transfer-suggestions', method: 'GET', params, signal })
      },
      /** PATCH /api/v1/transfer-suggestions/{id}/accept */
      acceptTransferSuggestion(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transfer-suggestions/${id}/accept`, method: 'PATCH', data, signal })
      },
      /** POST /api/v1/transfer-suggestions/{id}/cancel */
      cancelTransferSuggestion(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transfer-suggestions/${id}/cancel`, method: 'POST', data, signal })
      },
      /** POST /api/v1/transfer-suggestions/{id}/confirm-match */
      confirmTransferSuggestionMatch(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transfer-suggestions/${id}/confirm-match`, method: 'POST', data, signal })
      },
      /** POST /api/v1/transfer-suggestions/{id}/decline-match */
      declineTransferSuggestionMatch(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transfer-suggestions/${id}/decline-match`, method: 'POST', data, signal })
      },
      /** PATCH /api/v1/transfer-suggestions/{id}/reject */
      rejectTransferSuggestion(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/transfer-suggestions/${id}/reject`, method: 'PATCH', data, signal })
      },
    } as const,
    userAccounts: {
      /** GET /api/v1/user-accounts */
      getUserAccounts(params?: Record<string, unknown>, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: '/api/v1/user-accounts', method: 'GET', params, signal })
      },
      /** GET /api/v1/user-accounts/{id} */
      getUserAccountById(id: string, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/user-accounts/${id}`, method: 'GET', signal })
      },
      /** PATCH /api/v1/user-accounts/{id} */
      updateUserAccount(id: string, data?: unknown, signal?: AbortSignal): Promise<unknown> {
        return mutator({ url: `/api/v1/user-accounts/${id}`, method: 'PATCH', data, signal })
      },
    } as const,
  } as const
}

export type ApiClient = ReturnType<typeof createApiClient>

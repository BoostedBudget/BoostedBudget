import { defineConfig, mergeConfig } from 'vite'
import { defineConfig as defineVitestConfig } from 'vitest/config'
import viteConfig from './vite.config'

export default mergeConfig(
  viteConfig,
  defineVitestConfig({
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: './src/test/setup.ts',
      testTimeout: 30000, // Increased for slower CI (Issue #422: single fork mode)
      teardownTimeout: 30000, // V8 coverage cleanup can exceed 10s in CI
      pool: 'forks',
      poolOptions: {
        forks: {
          singleFork: true, // Reduce overhead, prevent zombie workers
          isolate: false, // Reuse process for all tests
        },
        threads: {
          singleThread: true,
        },
      },
      include: ['src/**/*.{test,spec}.{ts,tsx}', 'scripts/**/*.test.ts'],
      exclude: [
        'node_modules/**',
        'dist/**',
        'e2e/**',
        '**/*.config.*',
        'scripts/integration/**',
        // Issue #575: Exclude bun:test files during hybrid migration
        'src/features/subscription/hooks/useTierLimits.test.ts',
        'src/features/budgets/components/BudgetProgressBar.test.tsx',
        // Batch 1 - Pure logic tests (2025-01-08)
        'src/config/regions.test.ts',
        'src/shared/utils/string.test.ts',
        'src/lib/category-colors.test.ts',
        'src/privacy/utils.test.ts',
        'src/shared/charts/formatters.test.ts',
        'src/shared/charts/layout.test.ts',
        'src/features/auto-categorization/utils/categoryUtils.test.ts',
        'src/features/dashboard/types/gridstack-utils.test.ts',
        'src/features/budgets/utils/transactionBudgetMatcher.test.ts',
        // Batch 2 - Component tests without vi.* patterns (2026-01-08)
        'src/features/budgets/components/MilestoneIndicator.test.tsx',
        'src/features/accounts/components/AccountIconDisplay.test.tsx',
        'src/shared/hooks/useTabState.test.tsx',
        'src/features/budgets/components/transaction-link/TransactionBudgetBadge.test.tsx',
        'src/features/budgets/components/category-management/AllocationBreakdown.test.tsx',
        'src/features/budgets/components/budget-detail/BudgetIncomeSection.test.tsx',
        'src/features/rules/hooks/useRuleTierLimits.test.ts',
        'src/features/rules/hooks/useRuleForm.test.ts',
        // Batch 3 - Dashboard component tests (2026-01-08)
        'src/features/dashboard/components/AccountBalanceCards.test.tsx',
        'src/features/dashboard/components/BalanceOverview.test.tsx',
        // Batch 4 - Simple account component tests (2026-01-08)
        'src/features/accounts/AccountCard.test.tsx',
        // Batch 5 - Tests with simple vi.* patterns (2026-01-08)
        'src/features/accounts/components/EmojiInput.test.tsx',
        'src/features/accounts/components/IconSelector.test.tsx',
        // Batch 6 - Tests with vi.mock patterns (2026-01-08)
        'src/features/accounts/components/ColorPicker.test.tsx',
        'src/features/accounts/SyncStatsCard.test.tsx',
        // Batch 7 - Hook tests with vi.mock patterns (2026-01-08)
        'src/features/budgets/hooks/useCurrentBudget.test.tsx',
        'src/features/budgets/hooks/useBudgetTransactions.test.tsx',
        // Batch 8 - Dashboard tests (2026-01-08)
        'src/features/dashboard/components/AccountTypeFilter.test.tsx',
        'src/features/dashboard/hooks/useDashboardOnboarding.test.ts',
        // Batch 9 - Router tests (2026-01-08)
        'src/components/layout/MobileNav.test.tsx',
        'src/components/layout/Navigation.test.tsx',
        'src/components/layout/UserDropdown.test.tsx',
        'src/features/budgets/components/BudgetCard.test.tsx',
        'src/features/profile/components/SubscriptionCard.test.tsx',
        // Batch 10 - More router/component tests (2026-01-08)
        'src/features/budgets/components/category-management/BudgetCategoryCard.test.tsx',
        'src/features/budgets/components/budget-detail/UnallocatedFundsCard.test.tsx',
        'src/features/transactions/components/TransactionCard.test.tsx',
        'src/features/budgets/components/transaction-link/BudgetTransactionList.test.tsx',
        // Batch 11 - Filter and widget tests (2026-01-08)
        'src/features/transactions/components/filters/FilterPill.test.tsx',
        'src/features/transactions/components/filters/TransactionTypeFilter.test.tsx',
        'src/features/transactions/components/filters/TransactionStatusFilter.test.tsx',
        'src/features/dashboard/widgets/WidgetWrapper.test.tsx',
        // Batch 12 - Delete dialogs and utility tests (2026-01-08)
        'src/features/budgets/components/DeleteBudgetDialog.test.tsx',
        'src/features/budgets/components/DeleteSavingsGoalDialog.test.tsx',
        'src/shared/charts/useDarkMode.test.ts',
        'src/features/budgets/components/SavingsGoalCard.test.tsx',
        // Note: registry.test.ts stays on vitest - mock.module() conflicts with WidgetWrapper mocks
        // Batch 13 - Auto-categorization and rule card tests (2026-01-08)
        'src/features/auto-categorization/components/AutoCategorizationRuleCard.test.tsx',
        'src/features/auto-categorization/components/AutoCategorizationRuleTable.test.tsx',
        'src/features/rules/components/RuleCard.test.tsx',
        'src/features/budgets/goalTemplates.test.ts',
        // Batch 14 - Rule component tests (2026-01-08)
        'src/features/rules/components/CronBuilder.test.tsx',
        'src/features/rules/components/TriggerSelector.test.tsx',
        'src/features/rules/components/RuleTestResults.test.tsx',
        'src/features/dashboard/widgets/RemoveWidgetDialog.test.tsx',
        // Batch 15 - Shared component tests (2026-01-08)
        'src/shared/components/ListViewToggle/ListViewToggle.test.tsx',
        'src/shared/components/TableHeaderCell/TableHeaderCell.test.tsx',
        'src/shared/components/SearchableSelect/SearchableSelect.test.tsx',
        'src/shared/components/ColumnFilterDropdown/ColumnFilterDropdown.test.tsx',
        // Batch 16 - Filter hook tests (2026-01-08)
        'src/features/budgets/hooks/useBudgetFilters.test.ts',
        'src/features/auto-categorization/hooks/useRuleFilters.test.ts',
        'src/features/accounts/hooks/useAccountFilters.test.ts',
        'src/features/transactions/hooks/useTransactionFilters.test.ts',
        // Batch 17 - Action form tests (2026-01-09)
        'src/features/rules/components/actions/RolloverActionForm.test.tsx',
        'src/features/rules/components/actions/BudgetAlertActionForm.test.tsx',
        'src/features/rules/components/actions/SavingsGoalActionForm.test.tsx',
        'src/features/rules/components/actions/TransferActionForm.test.tsx',
        // Batch 18 - Income source tests (2026-01-09)
        'src/features/budgets/components/income/IncomeSourceConfig.test.tsx',
        'src/features/budgets/components/income/IncomeSourceItem.test.tsx',
        'src/features/budgets/components/income/IncomeSourcePicker.test.tsx',
        'src/features/budgets/components/category-management/CategoryMatchConfig.test.tsx',
        // Batch 19 - Hook and filter tests (2026-01-09)
        'src/features/budgets/hooks/useIncomeTracking.test.ts',
        'src/features/transactions/components/filters/TransactionDateFilter.test.tsx',
        // Batch 20 - Dashboard and tags tests (2026-01-09)
        'src/features/dashboard/components/TimeframeSelector.test.tsx',
        'src/features/tags/components/TagEditor.test.tsx',
        // Batch 21 - Hook and widget tests (2026-01-09)
        'src/shared/hooks/useCookieState.test.tsx',
        'src/features/dashboard/widgets/WidgetCatalog.test.tsx',
        'src/features/budgets/hooks/useAvailableCategories.test.tsx',
        // Batch 22 - Savings goals and rule execution tests (2026-01-09)
        'src/features/budgets/hooks/useSavingsGoals.test.tsx',
        'src/features/rules/components/RuleExecutionHistory.test.tsx',
        // Batch 23 - Rule table and deletion tests (2026-01-09)
        'src/features/rules/components/RuleTable.test.tsx',
        'src/features/deletion/components/DeleteAccountSection.test.tsx',
        // Batch 24 - Chart tests (2026-01-09)
        'src/features/dashboard/components/BalanceHistoryChart.test.tsx',
        // Batch 25 - Table component tests (2026-01-09)
        'src/features/accounts/components/AccountTable.test.tsx',
        'src/features/transactions/components/TransactionTable.test.tsx',
        // Batch 26 - Transaction filter tests (2026-01-09)
        'src/features/transactions/components/filters/TransactionCategoryFilter.test.tsx',
        'src/features/transactions/components/filters/TransactionAmountFilter.test.tsx',
        'src/features/transactions/components/filters/TransactionAccountFilter.test.tsx',
        // Batch 27 - Dashboard widget tests (2026-01-09)
        'src/features/dashboard/widgets/BudgetOverviewWidget.test.tsx',
        'src/features/dashboard/widgets/BalanceOverviewWidget.test.tsx',
        'src/features/dashboard/widgets/CashFlowSummaryWidget.test.tsx',
        // Batch 28 - More dashboard widget tests (2026-01-09)
        'src/features/dashboard/widgets/NetWorthWidget.test.tsx',
        'src/features/dashboard/widgets/AccountCardsWidget.test.tsx',
        // Batch 29 - Hook and context tests (2026-01-09)
        'src/shared/hooks/useCopyToClipboard.test.tsx',
        // NOTE: DashboardContext.test.tsx stays on vitest - tests real implementation, centralized mock overrides it
        'src/features/budgets/hooks/useMilestoneTracking.test.tsx',
        'src/features/budgets/hooks/usePreviewCategories.test.tsx',
        // Batch 30 - Dashboard and auto-categorization tests (2026-01-09)
        'src/features/dashboard/widgets/BalanceHistoryWidget.test.tsx',
        'src/features/dashboard/hooks/useDefaultDashboardLayout.test.tsx',
        'src/features/auto-categorization/components/AutoCategorizationPage.test.tsx',
        'src/features/auto-categorization/components/AutoCategorizationPageWrapper.test.tsx',
        // Batch 31 - Budget components (2026-01-09)
        'src/features/budgets/components/BudgetTable.test.tsx',
        // Batch 32 - Rules and category management (2026-01-09)
        'src/features/rules/components/ConditionBuilder.test.tsx',
        // NOTE: BudgetCategoryManager stays on vitest - mocks BudgetCategoryCard causing pollution
        // NOTE: RulesPage stays on vitest - mocks useRuleTierLimits, useProfile, useSubscriptionTiers causing pollution
        // Batch 34 - Simple vi.mock() files (2026-01-09)
        'src/features/dashboard/widgets/CashFlowChartWidget.test.tsx',
        // DashboardGrid uses centralized registry mock now
        'src/features/dashboard/widgets/DashboardGrid.test.tsx',
        'src/features/budgets/components/category-management/BudgetCategoryForm.test.tsx',
        'src/features/budgets/components/CreateSavingsGoalDialog.test.tsx',
        // MSW tests migrated to centralized API mocks (2026-01-10)
        'src/features/deletion/hooks/useDeletion.test.tsx',
        'src/features/accounts/hooks/useAccountCustomization.test.tsx',
        'src/features/tags/hooks/useTags.test.tsx',
        'src/features/profile/hooks/useProfile.test.tsx',
        'src/features/subscription/hooks/useSubscriptionTiers.test.tsx',
        // Phase 3: vi.hoisted hook tests migrated (2026-01-10)
        'src/features/budgets/hooks/useBudgets.test.tsx',
        'src/features/budgets/hooks/useBudgetDetail.test.tsx',
        'src/features/dashboard/hooks/useCashFlowData.test.tsx',
        'src/features/dashboard/hooks/useDashboardData.test.tsx',
        'src/features/dashboard/hooks/useDashboardLayout.test.tsx',
        'src/features/auto-categorization/hooks/useAutoCategorizationRules.test.tsx',
        'src/features/transactions/hooks/useTransactions.test.tsx',
        // Phase 4A: Page tests migrated (2026-01-10)
        'src/features/deletion/components/CancelDeletionPage.test.tsx',
        'src/components/layout/Header.test.tsx',
        'src/features/categories/components/CategoryEditor.test.tsx',
        // Phase 4B: More page tests (2026-01-10)
        'src/features/profile/ProfileForm.test.tsx',
        'src/features/profile/ProfilePage.test.tsx',
        'src/features/pricing/PricingPage.test.tsx',
        'src/features/transactions/TransactionsPage.test.tsx',
        // Phase 5: Real-implementation tests (isolated config) (2026-01-11)
        'src/features/dashboard/widgets/registry.test.ts',
        'src/features/dashboard/context/DashboardContext.test.tsx',
      ],
      coverage: {
        provider: 'v8',
        reporter: ['text', 'json', 'json-summary', 'html', 'lcov'],
        exclude: [
          'node_modules/',
          'src/test/',
          '**/*.d.ts',
          '**/*.config.*',
          '**/dist/',
          '**/e2e/**',
          '**/*.spec.ts',
          'src/main.tsx', // Entry point
          'src/privacy/CookieConsent.tsx', // UI component
          'src/privacy/adsense.ts', // Third-party integration
          'src/store/store.ts', // Redux store config
        ],
        thresholds: {
          lines: 25,
          functions: 67,
          branches: 68,
          statements: 25,
        },
      },
    },
  })
)

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

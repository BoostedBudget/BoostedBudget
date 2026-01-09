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

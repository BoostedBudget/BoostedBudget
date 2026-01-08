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

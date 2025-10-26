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
      // Increase timeout for slower CI environments (GitHub Actions can be slow)
      // CI is 10x+ slower than local - some tests timing out even at 10000ms
      testTimeout: 20000,
      // Increase teardown timeout for pool cleanup with V8 coverage
      // V8 coverage provider can take >10s to cleanup in CI, especially with single fork mode
      teardownTimeout: 30000,
      // Pool options for CI - use single fork to reduce overhead and prevent timeouts
      poolOptions: {
        forks: {
          singleFork: true, // Use single fork for CI to reduce overhead
        },
        threads: {
          singleThread: true, // Use single thread as fallback
        },
      },
      // Test files in src directory and scripts directory, exclude E2E tests
      include: ['src/**/*.{test,spec}.{ts,tsx}', 'scripts/**/*.test.ts'],
      exclude: [
        'node_modules/**',
        'dist/**',
        'e2e/**', // Exclude E2E tests - these run with Playwright
        '**/*.config.*',
        'scripts/integration/**', // Exclude integration tests - these run separately with vitest.integration.config.ts
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
          // Temporarily exclude files not yet covered by tests (TODO: add tests)
          'src/main.tsx', // Entry point - difficult to test
          'src/privacy/CookieConsent.tsx', // UI component - needs component tests
          'src/privacy/adsense.ts', // Third-party integration - needs integration tests
          'src/store/store.ts', // Redux store config - low priority
        ],
        // Coverage thresholds - CI will fail if below these values
        // TODO: Gradually increase these thresholds as we add more tests
        thresholds: {
          lines: 25,
          functions: 70,
          branches: 80,
          statements: 25,
        },
      },
    },
  })
)

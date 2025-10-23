import { defineConfig, mergeConfig } from 'vite'
import { defineConfig as defineVitestConfig } from 'vitest/config'
import viteConfig from './vite.config'

export default mergeConfig(
  viteConfig,
  defineVitestConfig({
    test: {
      globals: true,
      environment: 'jsdom',
      globalSetup: './vitest.global-setup.ts',
      setupFiles: './src/test/setup.ts',
      // Test files in src directory and scripts directory, exclude E2E tests
      include: ['src/**/*.{test,spec}.{ts,tsx}', 'scripts/**/*.test.ts'],
      exclude: [
        'node_modules/**',
        'dist/**',
        'e2e/**', // Exclude E2E tests - these run with Playwright
        '**/*.config.*',
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

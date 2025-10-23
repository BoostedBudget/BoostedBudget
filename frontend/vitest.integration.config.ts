import { defineConfig } from 'vitest/config'
import path from 'path'

/**
 * Vitest configuration for integration tests
 *
 * These tests are isolated from the main unit test suite to prevent
 * file generation/deletion from interfering with parallel test execution.
 *
 * Key differences from main config:
 * - Only includes scripts/integration/** tests
 * - Runs in single fork (no parallelization)
 * - Uses Node environment (no jsdom needed)
 * - No coverage (tests build tooling, not app code)
 */
export default defineConfig({
  test: {
    globals: true,
    environment: 'node', // Integration tests don't need jsdom

    // Only include integration tests
    include: ['scripts/integration/**/*.test.ts'],

    // Exclude everything else
    exclude: [
      'node_modules/**',
      'dist/**',
      'src/**', // Exclude all src tests
      'e2e/**', // Exclude E2E tests
    ],

    // No coverage for integration tests (they test file generation, not app logic)
    coverage: {
      enabled: false
    },

    // Run integration tests sequentially in single worker
    // This ensures file generation doesn't race with other tests
    pool: 'forks',
    poolOptions: {
      forks: {
        singleFork: true, // Force single worker for complete isolation
      }
    },
  },

  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})

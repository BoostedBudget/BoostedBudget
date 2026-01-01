import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright E2E Test Configuration
 * See https://playwright.dev/docs/test-configuration
 *
 * Dynamic Port Allocation:
 * - E2E_FRONTEND_PORT: Override frontend port (default: 3000)
 * - E2E_BACKEND_PORT: Override backend port (default: 8080)
 * - E2E_POSTGRES_PORT: Override PostgreSQL port (dynamic, set by run-e2e-with-ports.sh)
 * - DATABASE_URL: PostgreSQL connection string (set by run-e2e-with-ports.sh)
 * - Use scripts/find-available-ports.js in CI to avoid port conflicts
 */

// Get dynamic ports and database URL from environment, fallback to defaults
const frontendPort = process.env.E2E_FRONTEND_PORT || '3000';
const backendPort = process.env.E2E_BACKEND_PORT || '8080';
const databaseUrl = process.env.DATABASE_URL || '';

export default defineConfig({
  // Test directory
  testDir: './e2e',

  // Maximum time one test can run
  timeout: 15 * 1000,

  // Run tests in files in parallel
  fullyParallel: true,

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,

  // Retry on CI only
  retries: process.env.CI ? 2 : 0,

  // Opt out of parallel tests on CI
  // Limit to 2 workers locally to prevent memory exhaustion (was 8 on 8-core system)
  // See: Memory leak investigation Oct 2025 - saves 1.5GB vs unlimited workers
  workers: process.env.CI ? 1 : 2,

  // Reporter to use
  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'playwright-report/results.json' }],
    ['list']
  ],

  // Shared settings for all the projects below
  use: {
    // Base URL to use in actions like `await page.goto('/')`
    baseURL: process.env.PLAYWRIGHT_BASE_URL || `http://localhost:${frontendPort}`,

    // Backend API URL
    apiURL: process.env.PLAYWRIGHT_API_URL || `http://localhost:${backendPort}`,

    // Collect trace when retrying the failed test
    // Disable locally to save memory (video buffering ~200MB), enable in CI for debugging
    trace: process.env.CI ? 'on-first-retry' : 'off',

    // Screenshot on failure
    // Disable locally to save memory, enable in CI for debugging
    screenshot: process.env.CI ? 'only-on-failure' : 'off',

    // Video on failure
    // Disable locally to save memory, enable in CI for debugging
    video: process.env.CI ? 'retain-on-failure' : 'off',
  },

  // Configure projects for major browsers
  projects: [
    // Clerk setup project - must run first to obtain testing token
    {
      name: 'setup',
      testMatch: /global-setup\.ts/,
    },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['setup'], // Ensure Clerk setup runs first
    },
  ],

  // Run your local dev server before starting the tests
  // Comment this out if you want to run tests against already running servers
  webServer: [
    {
      // Pass PORT, E2E_BACKEND_PORT, and VITE_CLERK_PUBLISHABLE_KEY env vars to Vite
      // E2E_BACKEND_PORT is needed for the Vite proxy configuration
      // VITE_CLERK_PUBLISHABLE_KEY enables Clerk authentication (Vite only exposes VITE_* vars)
      command: `PORT=${frontendPort} E2E_BACKEND_PORT=${backendPort} VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || ''} pnpm run dev`,
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
    },
    {
      // Pass PORT and DATABASE_URL env vars to backend for dynamic port allocation
      // DATABASE_URL is set by run-e2e-with-ports.sh when PostgreSQL container is available
      command: `cd ../backend && PORT=${backendPort} DATABASE_URL="${databaseUrl}" ./bin/server`,
      url: `http://localhost:${backendPort}/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
    },
  ],
});
